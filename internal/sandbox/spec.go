package sandbox

import (
	"wktbox/internal/config"
	"wktbox/internal/environment"
	"wktbox/internal/gitbridge"
	"wktbox/internal/ports"
)

type Spec struct {
	ID         string
	Name       string
	Branch     string
	Version    string
	Worktree   string
	Ports      ports.Block
	Config     config.Config
	ProjectEnv environment.ProjectEnv
	GitBridge  gitbridge.Bridge
	Timezone   string
	PUID       int
	PGID       int
}

type Files struct {
	ComposePath            string
	SandboxEnvPath         string
	ProjectEnvOverridePath string
	GatewayConfigPath      string
}

func (files Files) ComposeFiles() []string {
	paths := []string{files.ComposePath}
	if files.ProjectEnvOverridePath != "" {
		paths = append(paths, files.ProjectEnvOverridePath)
	}
	return paths
}
