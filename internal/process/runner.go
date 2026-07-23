package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Runner interface {
	Run(ctx context.Context, name string, arguments ...string) (Result, error)
}

type OSRunner struct{}

func (OSRunner) Run(ctx context.Context, name string, arguments ...string) (Result, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err == nil {
		return result, nil
	}

	result.ExitCode = -1
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	}
	return result, fmt.Errorf("run %s: %w", name, err)
}
