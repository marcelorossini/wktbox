//go:build !windows

package executor_test

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"wktbox/internal/executor"
)

func TestOSInteractiveRunnerForwardsInterruptToChildProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdoutReader.Close()
	result := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := (executor.OSInteractiveRunner{}).Run(ctx, executor.Invocation{
			Name: os.Args[0],
			Arguments: []string{
				"-test.run=TestExecutorSignalHelper",
				"--",
				"signal-helper",
			},
			Stdout: stdoutWriter,
			Stderr: io.Discard,
		})
		_ = stdoutWriter.Close()
		result <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()

	ready, err := bufio.NewReader(stdoutReader).ReadString('\n')
	if err != nil {
		t.Fatalf("wait for helper: %v", err)
	}
	if ready != "ready\n" {
		t.Fatalf("helper output = %q", ready)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.code != 130 {
		t.Fatalf("exit code = %d", got.code)
	}
}

func TestExecutorSignalHelper(t *testing.T) {
	found := false
	for _, argument := range os.Args {
		if argument == "signal-helper" {
			found = true
		}
	}
	if !found {
		return
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	fmt.Println("ready")
	<-interrupts
	os.Exit(130)
}
