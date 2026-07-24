package agentintegration

import (
	"errors"
	"io/fs"
)

type Target string

const (
	TargetAll    Target = "all"
	TargetCodex  Target = "codex"
	TargetClaude Target = "claude"
)

const SkillName = "wktbox-isolated-development"

const ManagedInstructionsBlock = `<!-- wktbox-agent:start -->
When development needs Docker isolation, independent Compose ports, browser or
integration testing, or a linked-worktree environment, use the
` + "`wktbox-isolated-development`" + ` skill. It also works in the current checkout;
creating a worktree is optional.
<!-- wktbox-agent:end -->`

var ErrConflict = errors.New(
	"installed Wktbox skill has local modifications",
)

type Options struct {
	Target Target
	DryRun bool
	Force  bool
}

type TargetStatus struct {
	Target           Target `json:"target"`
	Installed        bool   `json:"installed"`
	Version          string `json:"version,omitempty"`
	SkillPath        string `json:"skillPath"`
	InstructionsPath string `json:"instructionsPath"`
	ManagedBlock     bool   `json:"managedBlock"`
	ExpectedDigest   string `json:"expectedDigest"`
	ActualDigest     string `json:"actualDigest,omitempty"`
	Conflict         bool   `json:"conflict"`
	Changed          bool   `json:"changed,omitempty"`
}

type Report struct {
	Targets []TargetStatus `json:"targets"`
}

type Dependencies struct {
	HomeDir   func() (string, error)
	LookupEnv func(string) (string, bool)
	Assets    fs.FS
	Version   string
	WriteFile func(string, []byte, fs.FileMode) error
}

type targetPaths struct {
	skill        string
	instructions string
}
