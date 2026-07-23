package doctor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"wktbox/internal/config"
	"wktbox/internal/discovery"
	"wktbox/internal/environment"
	"wktbox/internal/gitbridge"
	"wktbox/internal/ports"
	"wktbox/internal/process"
)

const defaultMinimumDisk = uint64(1 << 30)

type HostInput struct {
	Runner        process.Runner
	Path          string
	ConfigPath    string
	EnvFile       string
	EnvTarget     string
	Environ       map[string]string
	Allocator     ports.Allocator
	UsedPorts     []ports.Block
	StateRoot     string
	DindImage     string
	MinimumDisk   uint64
	FreeDisk      func(string) (uint64, error)
	DindTLS       func(context.Context) error
	GitValidation func(context.Context) error
}

type HostProbes struct {
	input HostInput

	worktreeOnce sync.Once
	worktree     discovery.Worktree
	worktreeErr  error

	configOnce sync.Once
	config     config.Config
	configErr  error
}

func NewHostProbes(input HostInput) *HostProbes {
	if input.Path == "" {
		input.Path = "."
	}
	if input.Environ == nil {
		input.Environ = map[string]string{}
	}
	if input.Allocator == (ports.Allocator{}) {
		input.Allocator = ports.NewAllocator(23000, 10)
	}
	if input.MinimumDisk == 0 {
		input.MinimumDisk = defaultMinimumDisk
	}
	if input.FreeDisk == nil {
		input.FreeDisk = availableDiskBytes
	}
	return &HostProbes{input: input}
}

func (probes *HostProbes) Check(ctx context.Context, id CheckID) ProbeResult {
	if err := ctx.Err(); err != nil {
		return ResultFromError(err, "Run doctor again after the operation is cancelled.")
	}
	switch id {
	case CheckDocker:
		return probes.docker(ctx)
	case CheckCompose:
		return probes.compose(ctx)
	case CheckLinuxContainers:
		return probes.linuxContainers(ctx)
	case CheckBindMount:
		return probes.bindMount(ctx)
	case CheckWorktree:
		return probes.gitWorktree(ctx)
	case CheckProjectEnv:
		return probes.projectEnvironment(ctx)
	case CheckPorts:
		return probes.portBlock(ctx)
	case CheckDindPrivileged:
		return probes.dindPrivileged(ctx)
	case CheckDiskSpace:
		return probes.diskSpace()
	case CheckDindTLS:
		return probes.dindTLS(ctx)
	case CheckGitBridge:
		return probes.gitBridge(ctx)
	default:
		return ProbeResult{Status: Fail, Message: "unknown doctor check " + string(id)}
	}
}

func (probes *HostProbes) docker(ctx context.Context) ProbeResult {
	result, err := probes.run(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return ResultFromError(err, "Install or start Docker Desktop, then retry.")
	}
	version := strings.TrimSpace(result.Stdout)
	if version == "" {
		return ProbeResult{
			Status:      Fail,
			Message:     "Docker did not return a server version",
			Remediation: "Start the Docker daemon and retry.",
		}
	}
	return ProbeResult{Status: Pass, Message: "Docker server " + version + " is available"}
}

func (probes *HostProbes) compose(ctx context.Context) ProbeResult {
	result, err := probes.run(ctx, "docker", "compose", "version", "--short")
	if err != nil {
		return ResultFromError(err, "Install the Docker Compose CLI plugin and retry.")
	}
	version := strings.TrimSpace(result.Stdout)
	if version == "" {
		return ProbeResult{
			Status:      Fail,
			Message:     "Docker Compose did not return a version",
			Remediation: "Install the Docker Compose CLI plugin and retry.",
		}
	}
	return ProbeResult{Status: Pass, Message: "Docker Compose " + version + " is available"}
}

func (probes *HostProbes) linuxContainers(ctx context.Context) ProbeResult {
	result, err := probes.run(ctx, "docker", "info", "--format", "{{.OSType}}")
	if err != nil {
		return ResultFromError(err, "Start Docker with the Linux container engine.")
	}
	osType := strings.TrimSpace(result.Stdout)
	if osType != "linux" {
		return ProbeResult{
			Status:      Fail,
			Message:     fmt.Sprintf("Docker daemon reports container OS %q", osType),
			Remediation: "Switch Docker Desktop to Linux containers.",
		}
	}
	return ProbeResult{Status: Pass, Message: "Docker is using Linux containers"}
}

func (probes *HostProbes) bindMount(ctx context.Context) ProbeResult {
	worktree, err := probes.resolveWorktree(ctx)
	if err != nil {
		return ResultFromError(err, "Fix the worktree path before testing bind mounts.")
	}
	image := probes.dindImage(ctx)
	mount := "type=bind,source=" + worktree.Path + ",target=/workspace,readonly"
	_, err = probes.run(
		ctx,
		"docker",
		"run",
		"--rm",
		"--mount",
		mount,
		image,
		"test",
		"-d",
		"/workspace",
	)
	if err != nil {
		return ResultFromError(
			err,
			"Share the worktree drive with Docker Desktop and verify the path is local.",
		)
	}
	return ProbeResult{Status: Pass, Message: "Docker can bind-mount the worktree read-only"}
}

func (probes *HostProbes) gitWorktree(ctx context.Context) ProbeResult {
	worktree, err := probes.resolveWorktree(ctx)
	if err != nil {
		return ResultFromError(err, "Run wktbox from inside a valid Git worktree or pass --path.")
	}
	return ProbeResult{
		Status:  Pass,
		Message: fmt.Sprintf("Git worktree %s on branch %s", worktree.Path, worktree.Branch),
	}
}

func (probes *HostProbes) projectEnvironment(ctx context.Context) ProbeResult {
	worktree, err := probes.resolveWorktree(ctx)
	if err != nil {
		return ResultFromError(err, "Fix the worktree before validating project env.")
	}
	cfg, err := probes.loadConfig(ctx)
	if err != nil {
		return ResultFromError(err, "Fix .wktbox.yml or the selected configuration file.")
	}
	projectEnv, err := environment.Resolve(
		worktree.Path,
		cfg,
		probes.input.EnvFile,
		probes.input.EnvTarget,
	)
	if err != nil {
		return ResultFromError(
			err,
			"Use a regular env file and a target below /workspace or /run/wktbox.",
		)
	}
	if projectEnv.Source == "" {
		return ProbeResult{Status: Pass, Message: "No project env file is configured"}
	}
	return ProbeResult{
		Status:  Pass,
		Message: "Project env source and read-only target are valid",
	}
}

func (probes *HostProbes) portBlock(ctx context.Context) ProbeResult {
	block, err := probes.input.Allocator.Reserve(ctx, probes.input.UsedPorts)
	if err != nil {
		return ResultFromError(err, "Free one complete loopback port block or stop an unused box.")
	}
	return ProbeResult{
		Status: Pass,
		Message: fmt.Sprintf(
			"loopback ports %d-%d are available",
			block.Start,
			block.End(),
		),
	}
}

func (probes *HostProbes) dindPrivileged(ctx context.Context) ProbeResult {
	_, err := probes.run(
		ctx,
		"docker",
		"run",
		"--rm",
		"--privileged",
		probes.dindImage(ctx),
		"true",
	)
	if err != nil {
		return ResultFromError(
			err,
			"Allow privileged Linux containers for the Wktbox DinD runtime.",
		)
	}
	return ProbeResult{Status: Pass, Message: "Docker permits the privileged DinD runtime"}
}

func (probes *HostProbes) diskSpace() ProbeResult {
	available, err := probes.input.FreeDisk(probes.input.StateRoot)
	if err != nil {
		return ResultFromError(err, "Ensure the Wktbox state volume is accessible.")
	}
	minimum := probes.input.MinimumDisk
	if available < minimum {
		return ProbeResult{
			Status: Fail,
			Message: fmt.Sprintf(
				"%d MiB available; at least %d MiB is required",
				available>>20,
				minimum>>20,
			),
			Remediation: "Free Docker and host disk space before creating a box.",
		}
	}
	return ProbeResult{
		Status:  Pass,
		Message: fmt.Sprintf("%d MiB available for Wktbox state", available>>20),
	}
}

func (probes *HostProbes) dindTLS(ctx context.Context) ProbeResult {
	if probes.input.DindTLS == nil {
		return ProbeResult{
			Status:      Warn,
			Message:     "DinD TLS requires a running box and was not exercised",
			Remediation: "Run wktbox up, then run doctor again to validate /certs/client.",
		}
	}
	if err := probes.input.DindTLS(ctx); err != nil {
		return ResultFromError(
			err,
			"Restart the box and verify its docker health check and /certs/client volume.",
		)
	}
	return ProbeResult{Status: Pass, Message: "Webtop can reach DinD over mutual TLS"}
}

func (probes *HostProbes) gitBridge(ctx context.Context) ProbeResult {
	worktree, err := probes.resolveWorktree(ctx)
	if err != nil {
		return ResultFromError(err, "Fix the worktree before validating its Git bridge.")
	}
	cfg, err := probes.loadConfig(ctx)
	if err != nil {
		return ResultFromError(err, "Fix the Git mode in .wktbox.yml.")
	}
	bridge, err := gitbridge.Prepare(worktree, cfg.Git.Mode)
	if err != nil {
		return ResultFromError(err, "Use git.mode: host for this repository layout.")
	}
	if !bridge.RequiresValidation {
		return ProbeResult{
			Status:      Warn,
			Message:     "git.mode host keeps shared Git metadata outside the box",
			Remediation: "Run Git on the host; opt into git.mode: mounted only after validation.",
		}
	}
	if probes.input.GitValidation == nil {
		return ProbeResult{
			Status:      Warn,
			Message:     "git.mode mounted layout is valid but needs a running box",
			Remediation: "Run wktbox up, then run doctor again to validate Git inside Webtop.",
		}
	}
	if err := probes.input.GitValidation(ctx); err != nil {
		return ResultFromError(err, "Use git.mode: host until mounted mode passes validation.")
	}
	return ProbeResult{Status: Pass, Message: "Mounted Git bridge passed runtime validation"}
}

func (probes *HostProbes) resolveWorktree(
	ctx context.Context,
) (discovery.Worktree, error) {
	probes.worktreeOnce.Do(func() {
		if probes.input.Runner == nil {
			probes.worktreeErr = errors.New("process runner is not configured")
			return
		}
		probes.worktree, probes.worktreeErr = discovery.Discover(
			ctx,
			probes.input.Runner,
			probes.input.Path,
		)
	})
	return probes.worktree, probes.worktreeErr
}

func (probes *HostProbes) loadConfig(ctx context.Context) (config.Config, error) {
	probes.configOnce.Do(func() {
		worktree, err := probes.resolveWorktree(ctx)
		if err != nil {
			probes.configErr = err
			return
		}
		probes.config, probes.configErr = config.Load(
			worktree.Path,
			probes.input.Environ,
			config.Overrides{
				ConfigPath: probes.input.ConfigPath,
				EnvFile:    probes.input.EnvFile,
				EnvTarget:  probes.input.EnvTarget,
			},
		)
	})
	return probes.config, probes.configErr
}

func (probes *HostProbes) dindImage(ctx context.Context) string {
	if probes.input.DindImage != "" {
		return probes.input.DindImage
	}
	if cfg, err := probes.loadConfig(ctx); err == nil && cfg.Runtime.DindImage != "" {
		return cfg.Runtime.DindImage
	}
	return config.Default().Runtime.DindImage
}

func (probes *HostProbes) run(
	ctx context.Context,
	name string,
	arguments ...string,
) (process.Result, error) {
	if probes.input.Runner == nil {
		return process.Result{}, errors.New("process runner is not configured")
	}
	result, err := probes.input.Runner.Run(ctx, name, arguments...)
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		if detail != "" {
			return result, fmt.Errorf("%s: %s: %w", name, detail, err)
		}
		return result, err
	}
	return result, nil
}

func nearestExistingPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		value = "."
	}
	current, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	for {
		if _, err := filepath.EvalSymlinks(current); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent for %s", value)
		}
		current = parent
	}
}
