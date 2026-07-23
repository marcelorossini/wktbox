package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"wktbox/internal/cli"
)

func TestRunHelpListsCommandsWithoutTouchingRuntime(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, cli.Streams{
		In:  strings.NewReader(""),
		Out: &stdout,
		Err: &stderr,
	}, []string{"--help"})

	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "run") || !strings.Contains(stdout.String(), "doctor") {
		t.Fatalf("help = %s", stdout.String())
	}
}

func TestRunWritesStructuredErrorToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := run(nil, cli.Streams{
		In:  strings.NewReader(""),
		Out: &stdout,
		Err: &stderr,
	}, []string{"--json", "not-a-command"})

	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("stderr = %q: %v", stderr.String(), err)
	}
	if got.Error.Code != "runtime_error" || !strings.Contains(got.Error.Message, "unknown command") {
		t.Fatalf("error = %#v", got)
	}
}
