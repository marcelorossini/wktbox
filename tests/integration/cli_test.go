package integration_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltCLIExposesMVPCommandsAndStructuredErrors(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	binary := filepath.Join(t.TempDir(), "wktbox")
	build := exec.Command("go", "build", "-o", binary, "./cmd/wktbox")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	help := exec.Command(binary, "--help")
	help.Dir = root
	helpOutput, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, helpOutput)
	}
	for _, command := range []string{
		"up", "run", "exec", "compose", "shell", "open", "list",
		"status", "logs", "stop", "restart", "destroy", "doctor",
		"port",
	} {
		if !strings.Contains(string(helpOutput), command) {
			t.Errorf("help does not list %q:\n%s", command, helpOutput)
		}
	}

	invalid := exec.Command(binary, "--json", "not-a-command")
	invalid.Dir = root
	errorOutput, err := invalid.Output()
	if err == nil {
		t.Fatal("unknown command unexpectedly succeeded")
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(exitError.Stderr, &response); err != nil {
		t.Fatalf("stderr = %q: %v", exitError.Stderr, err)
	}
	if response.Error.Code != "runtime_error" || len(errorOutput) != 0 {
		t.Fatalf("response = %#v, stdout = %q", response, errorOutput)
	}
}
