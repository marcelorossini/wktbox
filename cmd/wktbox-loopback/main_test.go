package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestResolveRelayIPv4UsesDockerHostNameWhenAvailable(t *testing.T) {
	got, err := resolveRelayIPv4(
		"host-gateway",
		func(host string) ([]string, error) {
			if host != "host.docker.internal" {
				t.Fatalf("lookup host = %q", host)
			}
			return []string{"2001:db8::1", "192.0.2.10"}, nil
		},
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.0.2.10" {
		t.Fatalf("relay IPv4 = %q", got)
	}
}

func TestResolveRelayIPv4FallsBackToLinuxDefaultGateway(t *testing.T) {
	route := strings.NewReader(`Iface	Destination	Gateway	Flags	RefCnt	Use	Metric	Mask
eth0	00000000	010011AC	0003	0	0	0	00000000
`)
	got, err := resolveRelayIPv4(
		"host-gateway",
		func(string) ([]string, error) {
			return nil, &net.DNSError{Err: "not found"}
		},
		route,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "172.17.0.1" {
		t.Fatalf("relay IPv4 = %q", got)
	}
}

func TestResolveRelayIPv4RejectsMissingIPv4(t *testing.T) {
	_, err := resolveRelayIPv4(
		"relay.example",
		func(host string) ([]string, error) {
			return []string{"2001:db8::1"}, nil
		},
		strings.NewReader(""),
	)
	if err == nil || !strings.Contains(err.Error(), "has no IPv4 address") {
		t.Fatalf("error = %v", err)
	}
}

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

func TestDNSCommandsDelegateToInterconnectRuntime(t *testing.T) {
	for _, command := range []string{"dns-serve", "dns-probe"} {
		t.Run(command, func(t *testing.T) {
			called := ""
			deps := dependencies{
				dnsServe: func(context.Context) error {
					called = "dns-serve"
					return nil
				},
				dnsProbe: func(context.Context) error {
					called = "dns-probe"
					return nil
				},
			}
			code := execute(
				context.Background(),
				[]string{command},
				&bytes.Buffer{},
				&bytes.Buffer{},
				deps,
			)
			if code != 0 || called != command {
				t.Fatalf("code=%d called=%q", code, called)
			}
		})
	}
}

func TestPublishServeDelegatesToPublicationRuntime(t *testing.T) {
	called := false
	code := execute(
		context.Background(),
		[]string{"publish-serve"},
		&bytes.Buffer{},
		&bytes.Buffer{},
		dependencies{
			publishServe: func(context.Context) error {
				called = true
				return nil
			},
		},
	)
	if code != 0 || !called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}

func TestImportPreflightDecodesAndDelegatesCandidateBatch(t *testing.T) {
	want := []portforward.Mapping{{
		Name:          "api",
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    1234,
		TargetPort:    1234,
	}}
	body, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got []portforward.Mapping
	code := execute(
		context.Background(),
		[]string{
			"imports-preflight",
			"--mappings",
			base64.RawURLEncoding.EncodeToString(body),
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
		dependencies{
			preflight: func(
				_ context.Context,
				mappings []portforward.Mapping,
			) error {
				got = mappings
				return nil
			},
		},
	)
	if code != 0 || len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("code=%d mappings=%#v", code, got)
	}
}
