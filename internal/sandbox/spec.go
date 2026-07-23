package sandbox

import (
	"wktbox/internal/config"
	"wktbox/internal/environment"
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
	Timezone   string
	PUID       int
	PGID       int
}

type Files struct {
	ComposePath            string
	SandboxEnvPath         string
	ProjectEnvOverridePath string
}

func (files Files) ComposeFiles() []string {
	paths := []string{files.ComposePath}
	if files.ProjectEnvOverridePath != "" {
		paths = append(paths, files.ProjectEnvOverridePath)
	}
	return paths
}
