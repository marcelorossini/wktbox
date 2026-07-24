package loopback

import (
	"reflect"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
)

func TestContainerFromInspectConvertsPublishedPortMap(t *testing.T) {
	inspect := container.InspectResponse{
		ID:   "abc123",
		Name: "/backend",
		State: &container.State{
			Running: true,
			Pid:     42,
		},
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("3000/tcp"): {
					{HostPort: "8000"},
					{},
				},
				network.MustParsePort("5173/tcp"): {
					{HostPort: "5173"},
				},
				network.MustParsePort("5353/udp"): {
					{HostPort: "5353"},
				},
			},
		},
	}

	got := containerFromInspect(inspect)

	want := Container{
		ID:      "abc123",
		Name:    "backend",
		Running: true,
		PID:     42,
		Ports: []PortBinding{
			{ContainerPort: 5173, HostPort: 5173, Protocol: "tcp", Published: true},
			{ContainerPort: 5353, HostPort: 5353, Protocol: "udp", Published: true},
			{ContainerPort: 3000, HostPort: 8000, Protocol: "tcp", Published: true},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("container = %#v; want %#v", got, want)
	}
}

func TestContainerFromInspectIgnoresInvalidHostPorts(t *testing.T) {
	got := containerFromInspect(container.InspectResponse{
		ID:    "abc123",
		State: &container.State{Running: true},
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("8080/tcp"): {
					{HostPort: ""},
					{HostPort: "not-a-port"},
					{HostPort: "70000"},
				},
			},
		},
	})

	if len(got.Ports) != 0 {
		t.Fatalf("ports = %#v", got.Ports)
	}
}

func TestContainerFromInspectHandlesMissingStateAndNetworkSettings(t *testing.T) {
	got := containerFromInspect(container.InspectResponse{ID: "abc123"})

	if got.Running || len(got.Ports) != 0 {
		t.Fatalf("container = %#v", got)
	}
}
