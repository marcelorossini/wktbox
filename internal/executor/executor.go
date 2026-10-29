package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"wktbox/internal/compose"
	"wktbox/internal/state"
)

type Invocation struct {
	Name      string
	Arguments []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

type InteractiveRunner interface {
	Run(context.Context, Invocation) (int, error)
}

// WorkspaceUser is the Webtop desktop account. The image aligns its UID and
// GID with the host PUID and PGID, so files it creates in the workspace keep
// the host user's ownership.
const WorkspaceUser = "abc"

// launcher loads the Webtop login environment, such as NVM and Node.js, when
// the image provides it and then replaces itself with the child command, so
// arguments keep their boundaries without shell reconstruction.
var launcher = []string{
	"/bin/sh",
	"-c",
	`if [ -x /usr/local/bin/wktbox-exec ]; then exec /usr/local/bin/wktbox-exec "$@"; fi; exec "$@"`,
	"wktbox-exec",
}

type Options struct {
	HostCWD     string
	TTY         bool
	Root        bool
	Environment []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

type Executor struct {
	runner InteractiveRunner
}

func New(runner InteractiveRunner) Executor {
	return Executor{runner: runner}
}

func (executor Executor) Run(
	ctx context.Context,
	box state.BoxRecord,
	command []string,
	options Options,
) (int, error) {
	if len(command) == 0 {
		return -1, errors.New("child command is required")
	}
	containerCWD, err := MapWorkingDirectory(box.Worktree, options.HostCWD)
	if err != nil {
		return -1, err
	}

	arguments := compose.ProjectFor(box).Arguments()
	arguments = append(arguments, "exec")
	if !options.TTY {
		arguments = append(arguments, "-T")
	}
	if options.Root {
		// Webtop sets HOME=/config, the desktop user's home. Keep root state
		// such as Docker CLI configuration out of it.
		arguments = append(arguments, "--env", "HOME=/root")
	} else {
		arguments = append(arguments, "--user", WorkspaceUser)
	}
	arguments = append(arguments, "-w", containerCWD)
	for index, value := range options.Environment {
		if !validEnvironmentAssignment(value) {
			return -1, fmt.Errorf(
				"invalid command environment at position %d; expected KEY=VALUE",
				index+1,
			)
		}
		arguments = append(arguments, "--env", value)
	}
	arguments = append(arguments, "webtop")
	arguments = append(arguments, launcher...)
	arguments = append(arguments, command...)

	invocation := Invocation{
		Name:      "docker",
		Arguments: arguments,
		Stdin:     options.Stdin,
		Stdout:    options.Stdout,
		Stderr:    options.Stderr,
	}
	if invocation.Stdin == nil {
		invocation.Stdin = os.Stdin
	}
	if invocation.Stdout == nil {
		invocation.Stdout = os.Stdout
	}
	if invocation.Stderr == nil {
		invocation.Stderr = os.Stderr
	}
	return executor.runner.Run(ctx, invocation)
}

func MapWorkingDirectory(worktree string, hostCWD string) (string, error) {
	if strings.TrimSpace(worktree) == "" {
		return "", errors.New("worktree path is required")
	}
	if hostCWD == "" {
		return "/workspace", nil
	}

	absoluteWorktree, err := filepath.Abs(worktree)
	if err != nil {
		return "", fmt.Errorf("resolve worktree path: %w", err)
	}
	absoluteCWD, err := filepath.Abs(hostCWD)
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	absoluteWorktree = evaluatePath(absoluteWorktree)
	absoluteCWD = evaluatePath(absoluteCWD)

	relative, err := filepath.Rel(absoluteWorktree, absoluteCWD)
	if err != nil {
		return "/workspace", nil
	}
	if relative == "." {
		return "/workspace", nil
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "/workspace", nil
	}
	return path.Join("/workspace", filepath.ToSlash(relative)), nil
}

func evaluatePath(value string) string {
	value = filepath.Clean(value)
	if evaluated, err := filepath.EvalSymlinks(value); err == nil {
		return evaluated
	}
	return value
}

func validEnvironmentAssignment(value string) bool {
	key, _, found := strings.Cut(value, "=")
	if !found || key == "" {
		return false
	}
	for index, character := range key {
		if character == '_' ||
			(index == 0 && ((character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z'))) ||
			(index > 0 && ((character >= 'a' && character <= 'z') ||
				(character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9'))) {
			continue
		}
		return false
	}
	return true
}

type OSInteractiveRunner struct{}

func (OSInteractiveRunner) Run(
	ctx context.Context,
	invocation Invocation,
) (int, error) {
	if invocation.Name == "" {
		return -1, errors.New("executable name is required")
	}
	command := exec.CommandContext(ctx, invocation.Name, invocation.Arguments...)
	command.Stdin = invocation.Stdin
	command.Stdout = invocation.Stdout
	command.Stderr = invocation.Stderr
	prepareCommand(command)

	if err := command.Start(); err != nil {
		return -1, fmt.Errorf("start %s: %w", invocation.Name, err)
	}
	stopForwarding := forwardSignals(command.Process)
	err := command.Wait()
	stopForwarding()
	if err == nil {
		return 0, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		exitCode := exitError.ExitCode()
		if exitCode < 0 {
			exitCode = 1
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return exitCode, ctxErr
		}
		return exitCode, nil
	}
	return -1, fmt.Errorf("wait for %s: %w", invocation.Name, err)
}
