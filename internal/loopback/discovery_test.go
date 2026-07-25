package loopback_test

import (
	"errors"
	"reflect"
	"testing"

	"wktbox/internal/loopback"
)

func TestDiscoverUsesPublishedHostPortAndDeduplicatesOrigins(t *testing.T) {
	got := loopback.Discover([]loopback.Container{
		{
			ID:      "a",
			Name:    "/api",
			Running: true,
			Ports: []loopback.PortBinding{{
				ContainerPort: 3000,
				HostPort:      8000,
				Protocol:      "tcp",
				Published:     true,
			}},
		},
		{
			ID:      "b",
			Name:    "worker",
			Running: true,
			Ports: []loopback.PortBinding{{
				ContainerPort: 9000,
				HostPort:      8000,
				Protocol:      "tcp",
				Published:     true,
			}},
		},
	})

	want := []loopback.Publication{{
		Port:    8000,
		Target:  "docker:8000",
		Sources: []string{"api", "worker"},
	}}
	if !reflect.DeepEqual(got.Publications, want) {
		t.Fatalf("publications = %#v; want %#v", got.Publications, want)
	}
}

func TestDiscoverSortsPortsAndSourcesDeterministically(t *testing.T) {
	got := loopback.Discover([]loopback.Container{
		{
			Name:    "zeta",
			Running: true,
			Ports: []loopback.PortBinding{
				{HostPort: 8080, Protocol: "tcp", Published: true},
				{HostPort: 5173, Protocol: "tcp", Published: true},
			},
		},
		{
			Name:    "alpha",
			Running: true,
			Ports: []loopback.PortBinding{{
				HostPort:  8080,
				Protocol:  "tcp",
				Published: true,
			}},
		},
	})

	want := []loopback.Publication{
		{Port: 5173, Target: "docker:5173", Sources: []string{"zeta"}},
		{Port: 8080, Target: "docker:8080", Sources: []string{"alpha", "zeta"}},
	}
	if !reflect.DeepEqual(got.Publications, want) {
		t.Fatalf("publications = %#v; want %#v", got.Publications, want)
	}
}

func TestDiscoverRejectsStoppedUnpublishedInvalidAndUDP(t *testing.T) {
	got := loopback.Discover([]loopback.Container{
		{
			Name:    "stopped",
			Running: false,
			Ports: []loopback.PortBinding{{
				HostPort:  5173,
				Protocol:  "tcp",
				Published: true,
			}},
		},
		{
			Name:    "exposed",
			Running: true,
			Ports: []loopback.PortBinding{{
				ContainerPort: 8080,
				Protocol:      "tcp",
			}},
		},
		{
			Name:    "invalid",
			Running: true,
			Ports: []loopback.PortBinding{{
				Protocol:  "tcp",
				Published: true,
			}},
		},
		{
			Name:    "dns",
			Running: true,
			Ports: []loopback.PortBinding{{
				HostPort:  5353,
				Protocol:  "udp",
				Published: true,
			}},
		},
	})

	if len(got.Publications) != 0 {
		t.Fatalf("publications = %#v", got.Publications)
	}
	want := []loopback.Warning{{
		Code:    "udp_unsupported",
		Port:    5353,
		Source:  "dns",
		Message: "UDP publication dns:5353 is not proxied",
	}}
	if !reflect.DeepEqual(got.Warnings, want) {
		t.Fatalf("warnings = %#v; want %#v", got.Warnings, want)
	}
}

func TestDiscoverUsesContainerIDWhenNameIsEmpty(t *testing.T) {
	got := loopback.Discover([]loopback.Container{{
		ID:      "abc123",
		Running: true,
		Ports: []loopback.PortBinding{{
			HostPort:  3000,
			Protocol:  "TCP",
			Published: true,
		}},
	}})

	if got.Publications[0].Sources[0] != "abc123" {
		t.Fatalf("sources = %#v", got.Publications[0].Sources)
	}
}

func TestDiscoverPreservesNormalizesSortsAndDeduplicatesPublishedAddresses(
	t *testing.T,
) {
	got := loopback.Discover([]loopback.Container{{
		Name:    "frontend",
		Running: true,
		Ports: []loopback.PortBinding{
			{
				HostIP:    "127.0.0.1",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
			{
				HostIP:    "0.0.0.0",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
			{
				HostIP:    "::1",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
			{
				HostIP:    "::",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
			{
				HostIP:    "172.24.0.2",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
			{
				HostIP:    "127.0.0.1",
				HostPort:  5174,
				Protocol:  "tcp",
				Published: true,
			},
		},
	}})

	want := []loopback.Publication{{
		Port:    5174,
		Target:  "docker:5174",
		Sources: []string{"frontend"},
		Upstreams: []string{
			"127.0.0.1:5174",
			"172.24.0.2:5174",
			"[::1]:5174",
		},
	}}
	if !reflect.DeepEqual(got.Publications, want) {
		t.Fatalf("publications = %#v; want %#v", got.Publications, want)
	}
}

func TestUnavailableReturnsStructuredTransientStatus(t *testing.T) {
	got := loopback.Unavailable(errors.New("sidecar stopped"))

	if got.EventStream != loopback.EventStreamUnavailable {
		t.Fatalf("event stream = %q", got.EventStream)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("updated time is zero")
	}
	want := []loopback.Warning{{
		Code:    "loopback_unavailable",
		Message: "sidecar stopped",
	}}
	if !reflect.DeepEqual(got.Warnings, want) {
		t.Fatalf("warnings = %#v; want %#v", got.Warnings, want)
	}
}
