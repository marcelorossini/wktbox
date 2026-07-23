package process_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"wktbox/internal/process"
)

func TestOSRunnerPreservesArgumentBoundaries(t *testing.T) {
	result, err := (process.OSRunner{}).Run(
		context.Background(),
		os.Args[0],
		"-test.run=TestProcessHelper",
		"--",
		"a b",
		"$(must-not-expand)",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "a b|$(must-not-expand)" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
}

func TestOSRunnerReturnsChildExitCode(t *testing.T) {
	result, err := (process.OSRunner{}).Run(
		context.Background(),
		os.Args[0],
		"-test.run=TestProcessHelper",
		"--",
		"exit",
	)
	if err == nil {
		t.Fatal("expected command error")
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
}

func TestProcessHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return
	}

	arguments := os.Args[separator+1:]
	if len(arguments) == 1 && arguments[0] == "exit" {
		os.Exit(7)
	}
	fmt.Fprint(os.Stdout, strings.Join(arguments, "|"))
	os.Exit(0)
}
