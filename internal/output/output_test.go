package output_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"wktbox/internal/agentintegration"
	"wktbox/internal/loopback"
	"wktbox/internal/output"
	"wktbox/internal/ports"
	"wktbox/internal/prune"
	"wktbox/internal/state"
)

func TestWriteBoxJSONUsesStablePublicContract(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{
		JSON: true,
		Out:  &stdout,
	})
	box := state.BoxRecord{
		ID:             "a4f8c9137d2b",
		Name:           "feature-auth",
		Status:         state.Ready,
		Worktree:       `/repo trees/feature-auth`,
		Branch:         "feature/auth",
		Ports:          ports.Block{Start: 23000, Size: 10},
		GatewayEnabled: true,
		CreatedAt:      time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
		LastUsedAt:     time.Date(2026, 7, 23, 11, 0, 0, 0, time.UTC),
		Loopback: loopback.Status{
			EventStream: loopback.EventStreamConnected,
			Routes: []loopback.Route{{
				Port:     5173,
				Target:   "docker:5173",
				Sources:  []string{"frontend"},
				Protocol: "tcp",
				State:    loopback.RouteListening,
			}},
			Warnings: []loopback.Warning{},
		},
	}

	if err := renderer.Box(box); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
	}
	if got["id"] != box.ID || got["status"] != string(state.Ready) {
		t.Fatalf("unexpected box JSON: %#v", got)
	}
	urls, ok := got["urls"].(map[string]any)
	if !ok {
		t.Fatalf("urls = %#v", got["urls"])
	}
	if urls["webtop"] != "http://localhost:23000" {
		t.Fatalf("webtop URL = %#v", urls["webtop"])
	}
	if urls["gateway"] != "http://localhost:23003" {
		t.Fatalf("gateway URL = %#v", urls["gateway"])
	}
	if urls["browserCdp"] != "http://localhost:23004" {
		t.Fatalf("browser CDP URL = %#v", urls["browserCdp"])
	}
	portsData, ok := got["ports"].(map[string]any)
	if !ok || portsData["browserCdp"] != float64(23004) {
		t.Fatalf("ports = %#v", got["ports"])
	}
	if _, leaked := got["composePath"]; leaked {
		t.Fatalf("internal path leaked in public JSON: %#v", got)
	}
	loopbackData, ok := got["loopback"].(map[string]any)
	if !ok || loopbackData["eventStream"] != loopback.EventStreamConnected {
		t.Fatalf("loopback = %#v", got["loopback"])
	}
}

func TestWriteErrorJSONGoesToStderr(t *testing.T) {
	var stderr bytes.Buffer
	renderer := output.New(output.Options{
		JSON: true,
		Err:  &stderr,
	})

	if err := renderer.Error("box_not_ready", "Box is stopped."); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "box_not_ready" || got.Error.Message != "Box is stopped." {
		t.Fatalf("error JSON = %#v", got)
	}
}

func TestHumanBoxNeverPrintsInternalGeneratedPaths(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{Out: &stdout})
	box := state.BoxRecord{
		ID:             "a4f8c9137d2b",
		Name:           "feature-auth",
		Status:         state.Ready,
		Worktree:       "/repo",
		Ports:          ports.Block{Start: 23000, Size: 10},
		ComposePath:    "/state/secret/compose.yml",
		SandboxEnvPath: "/state/secret/sandbox.env",
	}

	if err := renderer.Box(box); err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	for _, forbidden := range []string{box.ComposePath, box.SandboxEnvPath} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("human output leaked %q: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, "feature-auth") || !strings.Contains(got, "http://localhost:23000") {
		t.Fatalf("human output missing public fields: %s", got)
	}
	if !strings.Contains(got, "Browser CDP: http://localhost:23004") {
		t.Fatalf("human output missing browser CDP URL: %s", got)
	}
}

func TestConnectionsHumanOutputShowsAdjacency(t *testing.T) {
	var stdout bytes.Buffer
	boxes := map[string]state.BoxRecord{
		"a4f8c9137d2b": {
			ID: "a4f8c9137d2b", Name: "frontend", Status: state.Ready,
		},
		"dfe31c662a91": {
			ID: "dfe31c662a91", Name: "api", Status: state.Ready,
		},
	}
	record := state.ConnectionRecord{
		ID:      "64f420a77f31",
		Name:    "dev-stack",
		Network: "wktbox-connect-64f420a77f31",
		Members: []string{"a4f8c9137d2b", "dfe31c662a91"},
		Status:  state.ConnectionReady,
	}

	err := output.New(output.Options{Out: &stdout}).Connections(
		[]state.ConnectionRecord{record},
		boxes,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Connection dev-stack (64f420a77f31) is ready",
		"Network: wktbox-connect-64f420a77f31",
		"frontend (a4f8c9137d2b) -> a4f8c9137d2b.wktbox [ready]",
		"api (dfe31c662a91) -> dfe31c662a91.wktbox [ready]",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestConnectionsJSONUsesStablePublicShape(t *testing.T) {
	var stdout bytes.Buffer
	boxes := map[string]state.BoxRecord{
		"a4f8c9137d2b": {
			ID: "a4f8c9137d2b", Name: "frontend", Status: state.Ready,
		},
	}
	record := state.ConnectionRecord{
		ID:           "64f420a77f31",
		Name:         "dev-stack",
		Network:      "wktbox-connect-64f420a77f31",
		Members:      []string{"a4f8c9137d2b"},
		Status:       state.ConnectionDegraded,
		CreatedAt:    time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
		ReconciledAt: time.Date(2026, 7, 23, 12, 0, 1, 0, time.UTC),
	}

	err := output.New(output.Options{JSON: true, Out: &stdout}).Connections(
		[]state.ConnectionRecord{record},
		boxes,
	)
	if err != nil {
		t.Fatal(err)
	}

	var got []output.ConnectionData
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 ||
		got[0].ID != record.ID ||
		got[0].State != state.ConnectionDegraded ||
		got[0].Members[0].Alias != "a4f8c9137d2b.wktbox" ||
		got[0].Members[0].Name != "frontend" {
		t.Fatalf("data = %#v", got)
	}
}

func TestConnectionsEmptyAndQuietBehavior(t *testing.T) {
	var human bytes.Buffer
	if err := output.New(output.Options{Out: &human}).Connections(
		nil,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if got := human.String(); got != "No connections found.\n" {
		t.Fatalf("human = %q", got)
	}

	var quiet bytes.Buffer
	if err := output.New(output.Options{Quiet: true, Out: &quiet}).Connections(
		[]state.ConnectionRecord{{ID: "64f420a77f31"}},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if quiet.Len() != 0 {
		t.Fatalf("quiet = %q", quiet.String())
	}
}

func TestQuietSuppressesHumanMessagesButNotJSONData(t *testing.T) {
	var human bytes.Buffer
	humanRenderer := output.New(output.Options{Quiet: true, Out: &human})
	if err := humanRenderer.Message("started"); err != nil {
		t.Fatal(err)
	}
	if human.Len() != 0 {
		t.Fatalf("quiet output = %q", human.String())
	}

	var structured bytes.Buffer
	jsonRenderer := output.New(output.Options{JSON: true, Quiet: true, Out: &structured})
	if err := jsonRenderer.Box(state.BoxRecord{ID: "a4f8c9137d2b"}); err != nil {
		t.Fatal(err)
	}
	if structured.Len() == 0 {
		t.Fatal("quiet suppressed structured data")
	}
}

func TestLoopbackSummaryUsesProtocolHintsAndReportsConflictsAndWarnings(t *testing.T) {
	var stderr bytes.Buffer
	renderer := output.New(output.Options{Err: &stderr})
	status := loopback.Status{
		EventStream: loopback.EventStreamConnected,
		Routes: []loopback.Route{
			{Port: 443, Target: "docker:443", State: loopback.RouteListening},
			{Port: 5173, Target: "docker:5173", State: loopback.RouteListening},
			{Port: 5432, Target: "docker:5432", State: loopback.RouteListening},
			{Port: 61000, Target: "docker:61000", State: loopback.RouteConflict, Error: "address in use"},
		},
		Warnings: []loopback.Warning{{
			Code:    "udp_unsupported",
			Message: "UDP publication dns:5353 is not proxied",
		}},
	}

	if err := renderer.LoopbackSummary(status); err != nil {
		t.Fatal(err)
	}

	got := stderr.String()
	for _, want := range []string{
		"Automatic localhost routes inside Webtop:",
		"https://localhost:443",
		"http://localhost:5173",
		"localhost:5432 -> docker:5432",
		"localhost:61000 conflict: address in use",
		"UDP publication dns:5353 is not proxied",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q:\n%s", want, got)
		}
	}
}

func TestLoopbackSummaryJSONGoesToStderr(t *testing.T) {
	var stderr bytes.Buffer
	renderer := output.New(output.Options{JSON: true, Err: &stderr})
	status := loopback.Status{
		EventStream: loopback.EventStreamConnected,
		Routes:      []loopback.Route{},
		Warnings:    []loopback.Warning{},
	}

	if err := renderer.LoopbackSummary(status); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Loopback loopback.Status `json:"loopback"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Loopback.EventStream != loopback.EventStreamConnected {
		t.Fatalf("loopback = %#v", got.Loopback)
	}
}

func TestQuietSuppressesLoopbackSummary(t *testing.T) {
	var stderr bytes.Buffer
	renderer := output.New(output.Options{Quiet: true, Err: &stderr})

	if err := renderer.LoopbackSummary(loopback.Status{
		EventStream: loopback.EventStreamConnected,
	}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAgentReportJSONUsesPublicSchema(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{JSON: true, Out: &stdout})
	report := agentintegration.Report{Targets: []agentintegration.TargetStatus{{
		Target:           agentintegration.TargetCodex,
		Installed:        true,
		Version:          "0.1.0",
		SkillPath:        "/home/test/.agents/skills/wktbox-isolated-development",
		InstructionsPath: "/home/test/.codex/AGENTS.md",
		ManagedBlock:     true,
		ExpectedDigest:   "sha256:expected",
		ActualDigest:     "sha256:actual",
		Conflict:         true,
	}}}

	if err := renderer.AgentReport(report); err != nil {
		t.Fatal(err)
	}

	var got agentintegration.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 1 ||
		got.Targets[0].SkillPath != report.Targets[0].SkillPath ||
		!got.Targets[0].Conflict {
		t.Fatalf("report = %#v", got)
	}
}

func TestHumanAgentReportIncludesPathsStateAndConflictRemediation(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{Out: &stdout})
	report := agentintegration.Report{Targets: []agentintegration.TargetStatus{{
		Target:           agentintegration.TargetClaude,
		Installed:        true,
		Version:          "0.1.0",
		SkillPath:        "/home/test/.claude/skills/wktbox-isolated-development",
		InstructionsPath: "/home/test/.claude/CLAUDE.md",
		ManagedBlock:     false,
		ExpectedDigest:   "sha256:expected",
		ActualDigest:     "sha256:actual",
		Conflict:         true,
	}}}

	if err := renderer.AgentReport(report); err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	for _, want := range []string{
		"claude",
		"installed",
		report.Targets[0].SkillPath,
		report.Targets[0].InstructionsPath,
		"conflict",
		"--force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("human report missing %q:\n%s", want, got)
		}
	}
}

func TestPruneReportJSONUsesPublicSchema(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{JSON: true, Out: &stdout})
	report := prune.Report{
		Candidates: []prune.Candidate{{
			ID:       "aaaaaaaaaaaa",
			Name:     "removed",
			Worktree: "/removed/worktree",
		}},
		Destroyed: []prune.Candidate{},
		Warnings: []prune.Warning{{
			ID:      "bbbbbbbbbbbb",
			Path:    "/private/worktree",
			Message: "permission denied",
		}},
	}

	if err := renderer.PruneReport(report, false); err != nil {
		t.Fatal(err)
	}

	var got prune.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, report) {
		t.Fatalf("report = %#v, want %#v", got, report)
	}
}

func TestHumanPruneDryRunListsCandidatesWarningsAndForceHint(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{Out: &stdout})
	report := prune.Report{
		Candidates: []prune.Candidate{{
			ID:       "aaaaaaaaaaaa",
			Name:     "removed",
			Worktree: "/removed/worktree",
		}},
		Destroyed: []prune.Candidate{},
		Warnings: []prune.Warning{{
			ID:      "bbbbbbbbbbbb",
			Path:    "/private/worktree",
			Message: "permission denied",
		}},
	}

	if err := renderer.PruneReport(report, false); err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	for _, want := range []string{
		"removed",
		"aaaaaaaaaaaa",
		"/removed/worktree",
		"warning",
		"permission denied",
		"--force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prune report missing %q:\n%s", want, got)
		}
	}
}

func TestHumanForcedPruneListsDestroyedCandidates(t *testing.T) {
	var stdout bytes.Buffer
	renderer := output.New(output.Options{Out: &stdout})
	candidate := prune.Candidate{
		ID:       "aaaaaaaaaaaa",
		Name:     "removed",
		Worktree: "/removed/worktree",
	}
	report := prune.Report{
		Candidates: []prune.Candidate{candidate},
		Destroyed:  []prune.Candidate{candidate},
	}

	if err := renderer.PruneReport(report, true); err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Destroyed") ||
		!strings.Contains(got, candidate.ID) ||
		strings.Contains(got, "--force") {
		t.Fatalf("forced prune report:\n%s", got)
	}
}
