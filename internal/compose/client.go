package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"wktbox/internal/ports"
	"wktbox/internal/process"
)

type RuntimeState string

const (
	Absent  RuntimeState = "absent"
	Running RuntimeState = "running"
	Stopped RuntimeState = "stopped"
)

type Project struct {
	Name           string
	Files          []string
	EnvFile        string
	GatewayEnabled bool
}

type ContainerStatus struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

type Status struct {
	Exists     bool
	State      RuntimeState
	Containers []ContainerStatus
}

func (status Status) Ready() bool {
	if !status.Exists || status.State != Running {
		return false
	}
	dockerReady := false
	webtopReady := false
	for _, container := range status.Containers {
		switch container.Service {
		case "docker":
			dockerReady = container.State == "running" && container.Health == "healthy"
		case "webtop":
			webtopReady = container.State == "running"
		}
	}
	return dockerReady && webtopReady
}

type ManagedProject struct {
	ID          string
	Worktree    string
	ProjectName string
	State       RuntimeState
	Healthy     bool
	Ports       ports.Block
}

type Backend interface {
	Up(context.Context, Project) error
	Start(context.Context, Project) error
	Stop(context.Context, Project) error
	Restart(context.Context, Project) error
	Down(context.Context, Project, bool) error
	Inspect(context.Context, Project) (Status, error)
	ListManaged(context.Context) ([]ManagedProject, error)
}

type Client struct {
	runner process.Runner
}

func NewClient(runner process.Runner) Client {
	return Client{runner: runner}
}

func (client Client) Up(ctx context.Context, project Project) error {
	_, err := client.run(ctx, project, "up", "-d", "--wait")
	return err
}

func (client Client) Start(ctx context.Context, project Project) error {
	_, err := client.run(ctx, project, "start", "--wait")
	return err
}

func (client Client) Stop(ctx context.Context, project Project) error {
	_, err := client.run(ctx, project, "stop")
	return err
}

func (client Client) Restart(ctx context.Context, project Project) error {
	_, err := client.run(ctx, project, "restart")
	return err
}

func (client Client) Down(ctx context.Context, project Project, volumes bool) error {
	arguments := []string{"down"}
	if volumes {
		arguments = append(arguments, "--volumes")
	}
	arguments = append(arguments, "--remove-orphans")
	_, err := client.run(ctx, project, arguments...)
	return err
}

func (client Client) Logs(
	ctx context.Context,
	project Project,
	service string,
	follow bool,
) (process.Result, error) {
	arguments := []string{"logs"}
	if follow {
		arguments = append(arguments, "--follow")
	}
	if service != "" {
		arguments = append(arguments, service)
	}
	return client.run(ctx, project, arguments...)
}

func (client Client) Inspect(ctx context.Context, project Project) (Status, error) {
	result, err := client.run(ctx, project, "ps", "--all", "--format", "json")
	if err != nil {
		return Status{}, err
	}
	containers, err := decodeContainerStatuses(result.Stdout)
	if err != nil {
		return Status{}, fmt.Errorf("decode Docker Compose status: %w", err)
	}
	return deriveStatus(containers), nil
}

const managedFormat = `{{.ID}}\t{{.Names}}\t{{.State}}\t{{.Label "io.wktbox.box-id"}}\t{{.Label "io.wktbox.worktree"}}\t{{.Label "com.docker.compose.project"}}\t{{.Label "com.docker.compose.service"}}\t{{.Status}}\t{{.Ports}}`

var webtopHTTPPortPattern = regexp.MustCompile(`(?:127\.0\.0\.1|\[::1\]):(\d+)->3000/tcp`)

func (client Client) ListManaged(ctx context.Context) ([]ManagedProject, error) {
	result, err := client.runner.Run(
		ctx,
		"docker",
		"ps",
		"-a",
		"--filter",
		"label=io.wktbox.managed=true",
		"--format",
		managedFormat,
	)
	if err != nil {
		return nil, commandError("list managed containers", result, err)
	}

	type accumulator struct {
		project       ManagedProject
		running       bool
		dockerHealthy bool
		webtopRunning bool
	}
	grouped := make(map[string]*accumulator)
	scanner := bufio.NewScanner(strings.NewReader(result.Stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			return nil, fmt.Errorf("decode managed container: expected 7 fields, got %d", len(fields))
		}
		id := fields[3]
		if id == "" {
			continue
		}
		entry := grouped[id]
		if entry == nil {
			entry = &accumulator{project: ManagedProject{
				ID:          id,
				Worktree:    fields[4],
				ProjectName: fields[5],
				State:       Stopped,
			}}
			grouped[id] = entry
		}
		service := fields[6]
		containerRunning := fields[2] == "running"
		entry.running = entry.running || containerRunning
		statusText := ""
		if len(fields) > 7 {
			statusText = fields[7]
		}
		switch service {
		case "docker":
			entry.dockerHealthy = containerRunning &&
				strings.Contains(strings.ToLower(statusText), "(healthy)")
		case "webtop":
			entry.webtopRunning = containerRunning
			if len(fields) > 8 {
				if match := webtopHTTPPortPattern.FindStringSubmatch(fields[8]); len(match) == 2 {
					if port, parseErr := strconv.Atoi(match[1]); parseErr == nil {
						entry.project.Ports = ports.Block{Start: port, Size: 10}
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan managed containers: %w", err)
	}

	projects := make([]ManagedProject, 0, len(grouped))
	for _, entry := range grouped {
		if entry.running {
			entry.project.State = Running
		}
		entry.project.Healthy = entry.dockerHealthy && entry.webtopRunning
		projects = append(projects, entry.project)
	}
	sort.Slice(projects, func(left int, right int) bool {
		return projects[left].ID < projects[right].ID
	})
	return projects, nil
}

func (client Client) run(
	ctx context.Context,
	project Project,
	action ...string,
) (process.Result, error) {
	if project.Name == "" || len(project.Files) == 0 || project.EnvFile == "" {
		return process.Result{}, errors.New("invalid Docker Compose project")
	}
	arguments := []string{"compose", "-p", project.Name, "--env-file", project.EnvFile}
	for _, file := range project.Files {
		arguments = append(arguments, "-f", file)
	}
	if project.GatewayEnabled {
		arguments = append(arguments, "--profile", "gateway")
	}
	arguments = append(arguments, action...)
	result, err := client.runner.Run(ctx, "docker", arguments...)
	if err != nil {
		return result, commandError("docker compose "+action[0], result, err)
	}
	return result, nil
}

func commandError(operation string, result process.Result, err error) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %s: %w", operation, detail, err)
}

func decodeContainerStatuses(output string) ([]ContainerStatus, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}
	var containers []ContainerStatus
	if strings.HasPrefix(output, "[") {
		if err := json.Unmarshal([]byte(output), &containers); err != nil {
			return nil, err
		}
		return containers, nil
	}

	decoder := json.NewDecoder(bytes.NewBufferString(output))
	for decoder.More() {
		var container ContainerStatus
		if err := decoder.Decode(&container); err != nil {
			return nil, err
		}
		containers = append(containers, container)
	}
	return containers, nil
}

func deriveStatus(containers []ContainerStatus) Status {
	if len(containers) == 0 {
		return Status{State: Absent}
	}
	status := Status{
		Exists:     true,
		State:      Stopped,
		Containers: containers,
	}
	for _, container := range containers {
		if container.State == "running" {
			status.State = Running
			break
		}
	}
	return status
}
