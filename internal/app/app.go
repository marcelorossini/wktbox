package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/config"
	"wktbox/internal/discovery"
	"wktbox/internal/environment"
	"wktbox/internal/executor"
	"wktbox/internal/identity"
	"wktbox/internal/lock"
	"wktbox/internal/output"
	"wktbox/internal/ports"
	"wktbox/internal/process"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
	"wktbox/internal/version"
)

var ErrDoctorUnavailable = errors.New("doctor is not available")

type Request struct {
	Path        string
	ConfigPath  string
	EnvFile     string
	EnvTarget   string
	Profile     string
	Environment []string
	Verbose     bool
}

type Resolution struct {
	Request  Request
	ID       string
	Worktree discovery.Worktree
	Spec     sandbox.Spec
}

type SandboxManager interface {
	EnsureAllocated(context.Context, sandbox.Spec, ports.Allocator) (state.BoxRecord, error)
	Inspect(context.Context, string) (state.BoxRecord, error)
	List(context.Context) ([]state.BoxRecord, error)
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Destroy(context.Context, string) error
	Touch(context.Context, string) error
}

type Options struct {
	ProcessRunner     process.Runner
	Manager           SandboxManager
	InteractiveRunner executor.InteractiveRunner
	Allocator         ports.Allocator
	Environ           map[string]string
	Version           string
	Timezone          string
	PUID              int
	PGID              int
	OpenURL           func(context.Context, string) error
}

type App struct {
	processRunner     process.Runner
	manager           SandboxManager
	executor          executor.Executor
	interactiveRunner executor.InteractiveRunner
	allocator         ports.Allocator
	environ           map[string]string
	version           string
	timezone          string
	puid              int
	pgid              int
	openURL           func(context.Context, string) error
}

func New(options Options) *App {
	processRunner := options.ProcessRunner
	if processRunner == nil {
		processRunner = process.OSRunner{}
	}
	interactiveRunner := options.InteractiveRunner
	if interactiveRunner == nil {
		interactiveRunner = executor.OSInteractiveRunner{}
	}
	allocator := options.Allocator
	if allocator == (ports.Allocator{}) {
		allocator = ports.NewAllocator(23000, 10)
	}
	environ := options.Environ
	if environ == nil {
		environ = environmentMap(os.Environ())
	}
	buildVersion := options.Version
	if buildVersion == "" {
		buildVersion = version.String()
	}
	timezone := options.Timezone
	if timezone == "" {
		timezone = os.Getenv("TZ")
	}
	if timezone == "" {
		timezone = "UTC"
	}
	openURL := options.OpenURL
	if openURL == nil {
		openURL = openLoopbackURL
	}

	return &App{
		processRunner:     processRunner,
		manager:           options.Manager,
		executor:          executor.New(interactiveRunner),
		interactiveRunner: interactiveRunner,
		allocator:         allocator,
		environ:           environ,
		version:           buildVersion,
		timezone:          timezone,
		puid:              options.PUID,
		pgid:              options.PGID,
		openURL:           openURL,
	}
}

func NewDefault() (*App, error) {
	root, err := StateRoot()
	if err != nil {
		return nil, err
	}
	runner := process.OSRunner{}
	backend := compose.NewClient(runner)
	store := state.NewStore(root)
	manager := sandbox.NewManager(
		backend,
		store,
		lock.NewManager(root),
		time.Now,
	)
	puid, pgid := hostIDs()
	return New(Options{
		ProcessRunner:     runner,
		Manager:           manager,
		InteractiveRunner: executor.OSInteractiveRunner{},
		Allocator:         ports.NewAllocator(23000, 10),
		PUID:              puid,
		PGID:              pgid,
	}), nil
}

func (application *App) Resolve(
	ctx context.Context,
	request Request,
) (Resolution, error) {
	if request.Path == "" {
		request.Path = "."
	}
	worktree, err := discovery.Discover(ctx, application.processRunner, request.Path)
	if err != nil {
		return Resolution{}, err
	}
	cfg, err := config.Load(worktree.Path, application.environ, config.Overrides{
		ConfigPath: request.ConfigPath,
		EnvFile:    request.EnvFile,
		EnvTarget:  request.EnvTarget,
	})
	if err != nil {
		return Resolution{}, err
	}
	projectEnv, err := environment.Resolve(
		worktree.Path,
		cfg,
		request.EnvFile,
		request.EnvTarget,
	)
	if err != nil {
		return Resolution{}, err
	}
	platform := identity.Unix
	if runtime.GOOS == "windows" {
		platform = identity.Windows
	}
	boxIdentity := identity.ForWorktree(worktree.CommonDir, worktree.Path, platform)
	spec := sandbox.Spec{
		ID:         boxIdentity.ID,
		Name:       worktree.DisplayName,
		Branch:     worktree.Branch,
		Version:    application.version,
		Worktree:   worktree.Path,
		Config:     cfg,
		ProjectEnv: projectEnv,
		Timezone:   application.timezone,
		PUID:       application.puid,
		PGID:       application.pgid,
	}
	return Resolution{
		Request:  request,
		ID:       boxIdentity.ID,
		Worktree: worktree,
		Spec:     spec,
	}, nil
}

func (application *App) Ensure(
	ctx context.Context,
	resolution Resolution,
) (state.BoxRecord, error) {
	if application.manager == nil {
		return state.BoxRecord{}, errors.New("sandbox manager is not configured")
	}
	return application.manager.EnsureAllocated(ctx, resolution.Spec, application.allocator)
}

func (application *App) Inspect(
	ctx context.Context,
	resolution Resolution,
) (state.BoxRecord, error) {
	if application.manager == nil {
		return state.BoxRecord{}, errors.New("sandbox manager is not configured")
	}
	return application.manager.Inspect(ctx, resolution.ID)
}

func (application *App) List(ctx context.Context) ([]state.BoxRecord, error) {
	if application.manager == nil {
		return nil, errors.New("sandbox manager is not configured")
	}
	return application.manager.List(ctx)
}

func (application *App) Run(
	ctx context.Context,
	box state.BoxRecord,
	command []string,
	options executor.Options,
) (int, error) {
	code, runErr := application.executor.Run(ctx, box, command, options)
	touchErr := application.touch(ctx, box.ID)
	if runErr != nil && touchErr != nil {
		return code, errors.Join(runErr, touchErr)
	}
	if runErr != nil {
		return code, runErr
	}
	if touchErr != nil {
		return code, touchErr
	}
	return code, nil
}

func (application *App) Logs(
	ctx context.Context,
	box state.BoxRecord,
	service string,
	follow bool,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	arguments := composeArguments(box)
	arguments = append(arguments, "logs")
	if follow {
		arguments = append(arguments, "--follow")
	}
	if service != "" {
		arguments = append(arguments, service)
	}
	code, runErr := application.interactiveRunner.Run(ctx, executor.Invocation{
		Name:      "docker",
		Arguments: arguments,
		Stdout:    stdout,
		Stderr:    stderr,
	})
	touchErr := application.touch(ctx, box.ID)
	if runErr != nil && touchErr != nil {
		return code, errors.Join(runErr, touchErr)
	}
	if runErr != nil {
		return code, runErr
	}
	if touchErr != nil {
		return code, touchErr
	}
	return code, nil
}

func (application *App) Stop(ctx context.Context, resolution Resolution) error {
	return application.manager.Stop(ctx, resolution.ID)
}

func (application *App) Restart(ctx context.Context, resolution Resolution) error {
	return application.manager.Restart(ctx, resolution.ID)
}

func (application *App) Destroy(ctx context.Context, resolution Resolution) error {
	return application.manager.Destroy(ctx, resolution.ID)
}

func (application *App) Open(ctx context.Context, box state.BoxRecord) error {
	url := output.WebtopURL(box)
	if url == "" {
		return errors.New("box does not have a Webtop URL")
	}
	if err := application.openURL(ctx, url); err != nil {
		return fmt.Errorf("open Webtop URL: %w", err)
	}
	return nil
}

func (application *App) Doctor(context.Context, Request) (any, error) {
	return nil, fmt.Errorf("%w; run a newer build or use the host checks from the documentation", ErrDoctorUnavailable)
}

func (application *App) touch(ctx context.Context, id string) error {
	if application.manager == nil {
		return errors.New("sandbox manager is not configured")
	}
	return application.manager.Touch(ctx, id)
}

func composeArguments(box state.BoxRecord) []string {
	arguments := []string{
		"compose",
		"-p", box.ProjectName,
		"--env-file", box.SandboxEnvPath,
		"-f", box.ComposePath,
	}
	if box.ProjectEnvOverridePath != "" {
		arguments = append(arguments, "-f", box.ProjectEnvOverridePath)
	}
	if box.GatewayEnabled {
		arguments = append(arguments, "--profile", "gateway")
	}
	return arguments
}

func StateRoot() (string, error) {
	if override := os.Getenv("WKTBOX_STATE_HOME"); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("resolve WKTBOX_STATE_HOME: %w", err)
		}
		return filepath.Clean(absolute), nil
	}

	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is required to locate Wktbox state")
		}
		return filepath.Join(base, "Wktbox"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Wktbox"), nil
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "wktbox"), nil
	}
	return filepath.Join(home, ".local", "state", "wktbox"), nil
}

func environmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, content, found := strings.Cut(value, "=")
		if found {
			result[key] = content
		}
	}
	return result
}

func openLoopbackURL(ctx context.Context, url string) error {
	var name string
	var arguments []string
	switch runtime.GOOS {
	case "windows":
		name = "rundll32"
		arguments = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		name = "open"
		arguments = []string{url}
	default:
		name = "xdg-open"
		arguments = []string{url}
	}
	if err := exec.CommandContext(ctx, name, arguments...).Start(); err != nil {
		return err
	}
	return nil
}
