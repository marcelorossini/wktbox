package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"wktbox/internal/loopback"
)

func TestStatusJSONPrintsStatusAndSucceedsWhenConnected(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	requested := ""
	code := execute(
		context.Background(),
		[]string{"status", "--json"},
		&stdout,
		&stderr,
		dependencies{
			request: func(_ context.Context, command string) (loopback.Status, error) {
				requested = command
				return loopback.Status{
					EventStream: loopback.EventStreamConnected,
					Routes:      []loopback.Route{},
					Warnings:    []loopback.Warning{},
				}, nil
			},
		},
	)

	if code != 0 || requested != "status" {
		t.Fatalf("code=%d requested=%q stderr=%q", code, requested, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"eventStream":"connected"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestStatusFailsHealthcheckWhenStreamIsDisconnected(t *testing.T) {
	var stderr bytes.Buffer
	code := execute(
		context.Background(),
		[]string{"status", "--json"},
		&bytes.Buffer{},
		&stderr,
		dependencies{
			request: func(context.Context, string) (loopback.Status, error) {
				return loopback.Status{
					EventStream: loopback.EventStreamDisconnected,
				}, nil
			},
		},
	)

	if code != 1 || !strings.Contains(stderr.String(), "event stream is disconnected") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestSyncRequestsImmediateReconciliation(t *testing.T) {
	requested := ""
	code := execute(
		context.Background(),
		[]string{"sync", "--json"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		dependencies{
			request: func(_ context.Context, command string) (loopback.Status, error) {
				requested = command
				return loopback.Status{EventStream: loopback.EventStreamConnected}, nil
			},
		},
	)

	if code != 0 || requested != "sync" {
		t.Fatalf("code=%d requested=%q", code, requested)
	}
}

func TestCommandReportsControlErrorAndInvalidArguments(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		deps dependencies
		code int
		want string
	}{
		{
			name: "control error",
			args: []string{"status"},
			deps: dependencies{request: func(context.Context, string) (loopback.Status, error) {
				return loopback.Status{}, errors.New("control unavailable")
			}},
			code: 1,
			want: "control unavailable",
		},
		{
			name: "unknown command",
			args: []string{"reload"},
			deps: dependencies{},
			code: 2,
			want: "usage:",
		},
		{
			name: "invalid flag",
			args: []string{"sync", "--verbose"},
			deps: dependencies{},
			code: 2,
			want: "usage:",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := execute(
				context.Background(),
				test.args,
				&bytes.Buffer{},
				&stderr,
				test.deps,
			)
			if code != test.code || !strings.Contains(strings.ToLower(stderr.String()), test.want) {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}
