package doctor

import (
	"context"
	"fmt"
	"strings"
)

const RequiredCheckCount = 11

type CheckID string

const (
	CheckDocker          CheckID = "docker"
	CheckCompose         CheckID = "compose"
	CheckLinuxContainers CheckID = "linux_containers"
	CheckBindMount       CheckID = "bind_mount"
	CheckWorktree        CheckID = "worktree"
	CheckProjectEnv      CheckID = "project_env"
	CheckPorts           CheckID = "ports"
	CheckDindPrivileged  CheckID = "dind_privileged"
	CheckDiskSpace       CheckID = "disk_space"
	CheckDindTLS         CheckID = "dind_tls"
	CheckGitBridge       CheckID = "git_bridge"
)

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

type ProbeResult struct {
	Status      Status
	Message     string
	Remediation string
}

type Probes interface {
	Check(context.Context, CheckID) ProbeResult
}

type Input struct {
	Probes Probes
}

type Check struct {
	ID          CheckID `json:"id"`
	Name        string  `json:"name"`
	Status      Status  `json:"status"`
	Message     string  `json:"message"`
	Remediation string  `json:"remediation,omitempty"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

var orderedChecks = []struct {
	id   CheckID
	name string
}{
	{CheckDocker, "Docker"},
	{CheckCompose, "Docker Compose v2"},
	{CheckLinuxContainers, "Linux containers"},
	{CheckBindMount, "Workspace bind mount"},
	{CheckWorktree, "Workspace path"},
	{CheckProjectEnv, "Project environment"},
	{CheckPorts, "Loopback port block"},
	{CheckDindPrivileged, "Privileged DinD"},
	{CheckDiskSpace, "Disk space"},
	{CheckDindTLS, "DinD TLS"},
	{CheckGitBridge, "Git bridge"},
}

func Run(ctx context.Context, input Input) Report {
	report := Report{
		OK:     true,
		Checks: make([]Check, 0, RequiredCheckCount),
	}
	for _, definition := range orderedChecks {
		result := ProbeResult{
			Status:      Fail,
			Message:     "probe set is not configured",
			Remediation: "Run doctor through the wktbox CLI.",
		}
		if input.Probes != nil {
			result = safelyCheck(ctx, input.Probes, definition.id)
		}
		if result.Status != Pass && result.Status != Warn && result.Status != Fail {
			result = ProbeResult{
				Status:      Fail,
				Message:     "probe returned an invalid status",
				Remediation: "Update wktbox and run doctor again.",
			}
		}
		if strings.TrimSpace(result.Message) == "" {
			result.Message = string(result.Status)
		}
		if result.Status == Fail {
			report.OK = false
		}
		report.Checks = append(report.Checks, Check{
			ID:          definition.id,
			Name:        definition.name,
			Status:      result.Status,
			Message:     result.Message,
			Remediation: result.Remediation,
		})
	}
	return report
}

func ResultFromError(err error, remediation string) ProbeResult {
	if err == nil {
		return ProbeResult{Status: Pass, Message: "ok"}
	}
	return ProbeResult{
		Status:      Fail,
		Message:     err.Error(),
		Remediation: remediation,
	}
}

func (report Report) String() string {
	var result strings.Builder
	for _, check := range report.Checks {
		fmt.Fprintf(
			&result,
			"[%s] %s: %s\n",
			check.Status,
			check.Name,
			check.Message,
		)
		if check.Remediation != "" && check.Status != Pass {
			fmt.Fprintf(&result, "       %s\n", check.Remediation)
		}
	}
	if report.OK {
		result.WriteString("Doctor completed without blocking failures.")
	} else {
		result.WriteString("Doctor found blocking failures.")
	}
	return result.String()
}

func safelyCheck(
	ctx context.Context,
	probes Probes,
	id CheckID,
) (result ProbeResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = ProbeResult{
				Status:      Fail,
				Message:     fmt.Sprintf("probe panicked: %v", recovered),
				Remediation: "Run doctor with --verbose and report this failure.",
			}
		}
	}()
	return probes.Check(ctx, id)
}
