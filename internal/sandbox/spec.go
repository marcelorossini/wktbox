package sandbox

import (
	"wktbox/internal/config"
	"wktbox/internal/environment"
	"wktbox/internal/gitbridge"
	"wktbox/internal/portforward"
	"wktbox/internal/ports"
)

type Spec struct {
	ID           string
	Name         string
	Branch       string
	Version      string
	Worktree     string
	Ports        ports.Block
	Config       config.Config
	ProjectEnv   environment.ProjectEnv
	GitBridge    gitbridge.Bridge
	Timezone     string
	PUID         int
	PGID         int
	PortMappings []portforward.Mapping
}

type Files struct {
	ComposePath             string
	SandboxEnvPath          string
	ProjectEnvOverridePath  string
	GatewayConfigPath       string
	PortConfigPath          string
	PortOverridePath        string
	PortRelayTokenPath      string
	PortTransactionLockPath string
	Changed                 bool
}

func (files Files) ComposeFiles() []string {
	paths := []string{files.ComposePath}
	if files.ProjectEnvOverridePath != "" {
		paths = append(paths, files.ProjectEnvOverridePath)
	}
	if files.PortOverridePath != "" {
		paths = append(paths, files.PortOverridePath)
	}
	return paths
}
