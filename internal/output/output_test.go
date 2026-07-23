package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wktbox/internal/output"
	"wktbox/internal/ports"
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
	if _, leaked := got["composePath"]; leaked {
		t.Fatalf("internal path leaked in public JSON: %#v", got)
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
