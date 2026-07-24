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

	"wktbox/internal/loopback"
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
	loopbackReady := false
	interconnectReady := false
	for _, container := range status.Containers {
		switch container.Service {
		case "docker":
			dockerReady = container.State == "running" && container.Health == "healthy"
		case "webtop":
			webtopReady = container.State == "running"
		case "loopback":
			loopbackReady = container.State == "running" && container.Health == "healthy"
		case "interconnect":
			interconnectReady = container.State == "running" && container.Health == "healthy"
		}
	}
	return dockerReady && webtopReady && loopbackReady && interconnectReady
}

type ManagedProject struct {
	ID             string
	Worktree       string
	ProjectName    string
	State          RuntimeState
	Healthy        bool
	Ports          ports.Block
	GatewayEnabled bool
}

type ConnectionNetwork struct {
	ID         string
	Name       string
	DockerName string
	Version    string
}

type ConnectionEndpoint struct {
	Network string
	BoxID   string
	Service string
	Alias   string
}

type ConnectionNetworkStatus struct {
	Exists       bool
	Managed      bool
	ConnectionID string
	Endpoints    map[string]string
}

type Backend interface {
	Up(context.Context, Project) error
	Start(context.Context, Project) error
	Stop(context.Context, Project) error
	Restart(context.Context, Project) error
	Down(context.Context, Project, bool) error
	Inspect(context.Context, Project) (Status, error)
	ListManaged(context.Context) ([]ManagedProject, error)
	LoopbackStatus(context.Context, Project) (loopback.Status, error)
	LoopbackSync(context.Context, Project) (loopback.Status, error)
	EnsureConnectionNetwork(context.Context, ConnectionNetwork) error
	InspectConnectionNetwork(context.Context, string) (ConnectionNetworkStatus, error)
	ConnectConnectionEndpoint(context.Context, ConnectionEndpoint) error
	DisconnectConnectionEndpoint(context.Context, ConnectionEndpoint) error
	RemoveConnectionNetwork(context.Context, string) error
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

var webtopHTTPPortPattern = regexp.MustCompile(`(?:127\.0\.0\.1|\[::1\]):(\d+)->61000/tcp`)

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
		project             ManagedProject
		running             bool
		dockerHealthy       bool
		webtopRunning       bool
		loopbackHealthy     bool
		interconnectHealthy bool
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
		case "loopback":
			entry.loopbackHealthy = containerRunning &&
				strings.Contains(strings.ToLower(statusText), "(healthy)")
		case "interconnect":
			entry.interconnectHealthy = containerRunning &&
				strings.Contains(strings.ToLower(statusText), "(healthy)")
		case "gateway":
			entry.project.GatewayEnabled = true
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
		entry.project.Healthy = entry.dockerHealthy &&
			entry.webtopRunning &&
			entry.loopbackHealthy &&
			entry.interconnectHealthy
		projects = append(projects, entry.project)
	}
	sort.Slice(projects, func(left int, right int) bool {
		return projects[left].ID < projects[right].ID
	})
	return projects, nil
}

func (client Client) EnsureConnectionNetwork(
	ctx context.Context,
	network ConnectionNetwork,
) error {
	if network.ID == "" || network.DockerName == "" {
		return errors.New("invalid connection network")
	}
	status, err := client.InspectConnectionNetwork(ctx, network.DockerName)
	if err != nil {
		return err
	}
	if status.Exists {
		return validateConnectionNetwork(status, network)
	}

	arguments := []string{
		"network", "create",
		"--driver", "bridge",
		"--label", "io.wktbox.managed=true",
		"--label", "io.wktbox.connection-id=" + network.ID,
		"--label", "io.wktbox.connection-name=" + network.Name,
		"--label", "io.wktbox.version=" + network.Version,
		network.DockerName,
	}
	result, createErr := client.runner.Run(ctx, "docker", arguments...)
	if createErr == nil {
		return nil
	}
	if !containsAny(result, "already exists") {
		return commandError("create connection network", result, createErr)
	}
	status, err = client.InspectConnectionNetwork(ctx, network.DockerName)
	if err != nil {
		return err
	}
	return validateConnectionNetwork(status, network)
}

func (client Client) InspectConnectionNetwork(
	ctx context.Context,
	name string,
) (ConnectionNetworkStatus, error) {
	result, err := client.runner.Run(
		ctx,
		"docker",
		"network",
		"inspect",
		"--format",
		"{{json .}}",
		name,
	)
	if err != nil {
		if containsAny(result, "no such network", "not found") {
			return ConnectionNetworkStatus{
				Endpoints: make(map[string]string),
			}, nil
		}
		return ConnectionNetworkStatus{}, commandError(
			"inspect connection network",
			result,
			err,
		)
	}

	var document struct {
		Labels     map[string]string `json:"Labels"`
		Containers map[string]struct {
			Name string `json:"Name"`
		} `json:"Containers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &document); err != nil {
		return ConnectionNetworkStatus{}, fmt.Errorf(
			"decode connection network: %w",
			err,
		)
	}
	status := ConnectionNetworkStatus{
		Exists:       true,
		Managed:      document.Labels["io.wktbox.managed"] == "true",
		ConnectionID: document.Labels["io.wktbox.connection-id"],
		Endpoints:    make(map[string]string),
	}
	containerIDs := make([]string, 0, len(document.Containers))
	for containerID := range document.Containers {
		containerIDs = append(containerIDs, containerID)
	}
	sort.Strings(containerIDs)
	for _, containerID := range containerIDs {
		labels, err := client.containerLabels(ctx, containerID)
		if err != nil {
			return ConnectionNetworkStatus{}, err
		}
		boxID := labels["io.wktbox.box-id"]
		service := labels["com.docker.compose.service"]
		if boxID != "" && service != "" {
			status.Endpoints[boxID+"/"+service] = containerID
		}
	}
	return status, nil
}

func (client Client) ConnectConnectionEndpoint(
	ctx context.Context,
	endpoint ConnectionEndpoint,
) error {
	containerID, err := client.resolveServiceContainer(
		ctx,
		endpoint.BoxID,
		endpoint.Service,
		false,
	)
	if err != nil {
		return err
	}
	arguments := []string{"network", "connect"}
	if endpoint.Alias != "" {
		arguments = append(arguments, "--alias", endpoint.Alias)
	}
	arguments = append(arguments, endpoint.Network, containerID)
	result, err := client.runner.Run(ctx, "docker", arguments...)
	if err != nil && !containsAny(result, "already exists", "already connected") {
		return commandError("connect connection endpoint", result, err)
	}
	return nil
}

func (client Client) DisconnectConnectionEndpoint(
	ctx context.Context,
	endpoint ConnectionEndpoint,
) error {
	containerID, err := client.resolveServiceContainer(
		ctx,
		endpoint.BoxID,
		endpoint.Service,
		true,
	)
	if errors.Is(err, errServiceContainerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := client.runner.Run(
		ctx,
		"docker",
		"network",
		"disconnect",
		endpoint.Network,
		containerID,
	)
	if err != nil && !containsAny(
		result,
		"is not connected",
		"no such network",
		"not found",
	) {
		return commandError("disconnect connection endpoint", result, err)
	}
	return nil
}

func (client Client) RemoveConnectionNetwork(
	ctx context.Context,
	name string,
) error {
	result, err := client.runner.Run(ctx, "docker", "network", "rm", name)
	if err != nil && !containsAny(result, "no such network", "not found") {
		return commandError("remove connection network", result, err)
	}
	return nil
}

var errServiceContainerNotFound = errors.New("service container not found")

func (client Client) resolveServiceContainer(
	ctx context.Context,
	boxID string,
	service string,
	includeStopped bool,
) (string, error) {
	arguments := []string{"ps"}
	if includeStopped {
		arguments = append(arguments, "-a")
	}
	arguments = append(
		arguments,
		"--filter", "label=io.wktbox.box-id="+boxID,
		"--filter", "label=com.docker.compose.service="+service,
	)
	if !includeStopped {
		arguments = append(arguments, "--filter", "status=running")
	}
	arguments = append(arguments, "--format", "{{.ID}}")
	result, err := client.runner.Run(ctx, "docker", arguments...)
	if err != nil {
		return "", commandError("resolve connection endpoint", result, err)
	}
	var ids []string
	for _, line := range strings.Split(result.Stdout, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return "", fmt.Errorf(
			"%w: box %s service %s",
			errServiceContainerNotFound,
			boxID,
			service,
		)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf(
			"resolve connection endpoint: box %s service %s has %d containers",
			boxID,
			service,
			len(ids),
		)
	}
	return ids[0], nil
}

func (client Client) containerLabels(
	ctx context.Context,
	containerID string,
) (map[string]string, error) {
	result, err := client.runner.Run(
		ctx,
		"docker",
		"inspect",
		"--format",
		"{{json .Config.Labels}}",
		containerID,
	)
	if err != nil {
		return nil, commandError("inspect connection endpoint", result, err)
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &labels); err != nil {
		return nil, fmt.Errorf("decode connection endpoint labels: %w", err)
	}
	return labels, nil
}

func validateConnectionNetwork(
	status ConnectionNetworkStatus,
	network ConnectionNetwork,
) error {
	if !status.Exists || !status.Managed || status.ConnectionID != network.ID {
		return fmt.Errorf(
			"connection network %q exists but is not managed by Wktbox for connection %s",
			network.DockerName,
			network.ID,
		)
	}
	return nil
}

func containsAny(result process.Result, fragments ...string) bool {
	output := strings.ToLower(result.Stdout + "\n" + result.Stderr)
	for _, fragment := range fragments {
		if strings.Contains(output, strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func (client Client) LoopbackStatus(
	ctx context.Context,
	project Project,
) (loopback.Status, error) {
	return client.loopbackCommand(ctx, project, "status")
}

func (client Client) LoopbackSync(
	ctx context.Context,
	project Project,
) (loopback.Status, error) {
	return client.loopbackCommand(ctx, project, "sync")
}

func (client Client) loopbackCommand(
	ctx context.Context,
	project Project,
	command string,
) (loopback.Status, error) {
	result, err := client.run(
		ctx,
		project,
		"exec",
		"-T",
		"loopback",
		"wktbox-loopback",
		command,
		"--json",
	)
	if err != nil {
		return loopback.Status{}, err
	}
	var status loopback.Status
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		return loopback.Status{}, fmt.Errorf("decode loopback %s status: %w", command, err)
	}
	return status, nil
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
