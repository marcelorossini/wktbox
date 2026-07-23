package doctor_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"wktbox/internal/doctor"
)

type fakeProbes struct {
	results map[doctor.CheckID]doctor.ProbeResult
	calls   []doctor.CheckID
}

func (probes *fakeProbes) Check(
	_ context.Context,
	id doctor.CheckID,
) doctor.ProbeResult {
	probes.calls = append(probes.calls, id)
	if result, exists := probes.results[id]; exists {
		return result
	}
	return doctor.ProbeResult{Status: doctor.Pass, Message: "ok"}
}

func TestRunReportsAllChecksInsteadOfStoppingAtFirstFailure(t *testing.T) {
	probes := &fakeProbes{results: map[doctor.CheckID]doctor.ProbeResult{
		doctor.CheckDocker: {
			Status:      doctor.Fail,
			Message:     "Docker CLI was not found",
			Remediation: "Install Docker Desktop.",
		},
		doctor.CheckPorts: {
			Status:  doctor.Fail,
			Message: "no complete port block is available",
		},
	}}

	report := doctor.Run(context.Background(), doctor.Input{Probes: probes})

	if len(report.Checks) != doctor.RequiredCheckCount {
		t.Fatalf("checks = %d", len(report.Checks))
	}
	if len(probes.calls) != doctor.RequiredCheckCount {
		t.Fatalf("probe calls = %d", len(probes.calls))
	}
	if report.OK {
		t.Fatal("report should fail")
	}
	if report.Checks[0].ID != doctor.CheckDocker {
		t.Fatalf("first check = %s", report.Checks[0].ID)
	}
}

func TestWarningsAreVisibleButDoNotFailDoctor(t *testing.T) {
	probes := &fakeProbes{results: map[doctor.CheckID]doctor.ProbeResult{
		doctor.CheckGitBridge: {
			Status:      doctor.Warn,
			Message:     "git.mode host does not expose Git inside the box",
			Remediation: "Run Git on the host.",
		},
	}}

	report := doctor.Run(context.Background(), doctor.Input{Probes: probes})

	if !report.OK {
		t.Fatalf("warning failed report: %#v", report)
	}
	var bridge doctor.Check
	for _, check := range report.Checks {
		if check.ID == doctor.CheckGitBridge {
			bridge = check
		}
	}
	if bridge.Status != doctor.Warn || bridge.Remediation == "" {
		t.Fatalf("bridge check = %#v", bridge)
	}
}

func TestMissingProbeSetStillReturnsCompleteActionableReport(t *testing.T) {
	report := doctor.Run(context.Background(), doctor.Input{})

	if len(report.Checks) != doctor.RequiredCheckCount || report.OK {
		t.Fatalf("report = %#v", report)
	}
	for _, check := range report.Checks {
		if check.Status != doctor.Fail || check.Message == "" {
			t.Fatalf("check = %#v", check)
		}
	}
}

func TestCheckOrderIsStable(t *testing.T) {
	probes := &fakeProbes{}
	report := doctor.Run(context.Background(), doctor.Input{Probes: probes})
	got := make([]doctor.CheckID, 0, len(report.Checks))
	for _, check := range report.Checks {
		got = append(got, check.ID)
	}
	want := []doctor.CheckID{
		doctor.CheckDocker,
		doctor.CheckCompose,
		doctor.CheckLinuxContainers,
		doctor.CheckBindMount,
		doctor.CheckWorktree,
		doctor.CheckProjectEnv,
		doctor.CheckPorts,
		doctor.CheckDindPrivileged,
		doctor.CheckDiskSpace,
		doctor.CheckDindTLS,
		doctor.CheckGitBridge,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %#v", got)
	}
}

func TestProbePanicOrErrorIsRepresentedAsFailure(t *testing.T) {
	result := doctor.ResultFromError(errors.New("daemon unavailable"), "Start Docker.")
	if result.Status != doctor.Fail ||
		result.Message != "daemon unavailable" ||
		result.Remediation != "Start Docker." {
		t.Fatalf("result = %#v", result)
	}
}
