package loopback

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type DockerSource struct {
	client *client.Client
}

func NewDockerSourceFromEnv() (*DockerSource, error) {
	dockerClient, err := client.New(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create inner Docker client: %w", err)
	}
	return &DockerSource{client: dockerClient}, nil
}

func (source *DockerSource) Snapshot(ctx context.Context) ([]Container, error) {
	listed, err := source.client.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list inner Docker containers: %w", err)
	}
	containers := make([]Container, 0, len(listed.Items))
	for _, summary := range listed.Items {
		inspected, inspectErr := source.client.ContainerInspect(
			ctx,
			summary.ID,
			client.ContainerInspectOptions{},
		)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect inner Docker container %s: %w", summary.ID, inspectErr)
		}
		containers = append(containers, containerFromInspect(inspected.Container))
	}
	sort.Slice(containers, func(left, right int) bool {
		if containers[left].Name != containers[right].Name {
			return containers[left].Name < containers[right].Name
		}
		return containers[left].ID < containers[right].ID
	})
	return containers, nil
}

func (source *DockerSource) Subscribe(ctx context.Context) (Subscription, error) {
	filters := make(client.Filters)
	filters.Add("type", "container")
	filters.Add("event", "start", "die", "stop", "destroy", "rename")
	result := source.client.Events(ctx, client.EventsListOptions{Filters: filters})
	events := make(chan Event)
	errors := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errors)
		for {
			select {
			case <-ctx.Done():
				return
			case message, open := <-result.Messages:
				if !open {
					select {
					case errors <- io.EOF:
					case <-ctx.Done():
					}
					return
				}
				select {
				case events <- Event{Action: string(message.Action)}:
				case <-ctx.Done():
					return
				}
			case streamErr, open := <-result.Err:
				if !open {
					streamErr = io.EOF
				}
				if streamErr == nil {
					streamErr = io.EOF
				}
				select {
				case errors <- streamErr:
				case <-ctx.Done():
				}
				return
			}
		}
	}()
	return Subscription{Events: events, Errors: errors}, nil
}

func (source *DockerSource) Close() error {
	return source.client.Close()
}

func containerFromInspect(inspect container.InspectResponse) Container {
	result := Container{
		ID:   inspect.ID,
		Name: strings.TrimPrefix(inspect.Name, "/"),
	}
	if inspect.State != nil {
		result.Running = inspect.State.Running
	}
	if inspect.NetworkSettings == nil {
		return result
	}

	seen := make(map[PortBinding]struct{})
	for innerPort, bindings := range inspect.NetworkSettings.Ports {
		for _, binding := range bindings {
			hostPort, err := strconv.ParseUint(binding.HostPort, 10, 16)
			if err != nil || hostPort == 0 {
				continue
			}
			port := PortBinding{
				ContainerPort: innerPort.Num(),
				HostPort:      uint16(hostPort),
				Protocol:      string(innerPort.Proto()),
				Published:     true,
			}
			if _, exists := seen[port]; exists {
				continue
			}
			seen[port] = struct{}{}
			result.Ports = append(result.Ports, port)
		}
	}
	sort.Slice(result.Ports, func(left, right int) bool {
		if result.Ports[left].HostPort != result.Ports[right].HostPort {
			return result.Ports[left].HostPort < result.Ports[right].HostPort
		}
		if result.Ports[left].ContainerPort != result.Ports[right].ContainerPort {
			return result.Ports[left].ContainerPort < result.Ports[right].ContainerPort
		}
		return result.Ports[left].Protocol < result.Ports[right].Protocol
	})
	return result
}
