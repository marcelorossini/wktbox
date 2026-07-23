package state

import (
	"time"

	"wktbox/internal/ports"
)

type Status string

const (
	Creating   Status = "creating"
	Ready      Status = "ready"
	Stopping   Status = "stopping"
	Stopped    Status = "stopped"
	Starting   Status = "starting"
	Error      Status = "error"
	Destroying Status = "destroying"
)

type State struct {
	Version int                  `json:"version"`
	Boxes   map[string]BoxRecord `json:"boxes"`
}

type BoxRecord struct {
	ID                     string      `json:"id"`
	Name                   string      `json:"name"`
	Worktree               string      `json:"worktree"`
	Branch                 string      `json:"branch,omitempty"`
	ProjectName            string      `json:"projectName"`
	Status                 Status      `json:"status"`
	Ports                  ports.Block `json:"ports"`
	ComposePath            string      `json:"composePath"`
	SandboxEnvPath         string      `json:"sandboxEnvPath"`
	ProjectEnvOverridePath string      `json:"projectEnvOverridePath,omitempty"`
	GatewayEnabled         bool        `json:"gatewayEnabled,omitempty"`
	CreatedAt              time.Time   `json:"createdAt"`
	LastUsedAt             time.Time   `json:"lastUsedAt"`
}

func Empty() State {
	return State{
		Version: 1,
		Boxes:   make(map[string]BoxRecord),
	}
}
