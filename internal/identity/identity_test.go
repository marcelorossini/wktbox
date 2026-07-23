package identity_test

import (
	"testing"

	"wktbox/internal/identity"
)

func TestForWorktreeUsesCommonDirAndCanonicalWindowsPath(t *testing.T) {
	got := identity.ForWorktree(
		`C:\Repo\.git`,
		`C:\Repo Trees\Feature`,
		identity.Windows,
	)
	if got.ID != "ac315bb51ee6" {
		t.Fatalf("ID = %s", got.ID)
	}
	if got.ProjectName != "wktbox-ac315bb51ee6" {
		t.Fatalf("project name = %s", got.ProjectName)
	}
	if got.DisplayName != "feature" {
		t.Fatalf("display name = %s", got.DisplayName)
	}
}

func TestForWorktreeNormalizesWindowsCaseAndSeparators(t *testing.T) {
	first := identity.ForWorktree(
		`C:\Repo\.git`,
		`C:\Repo Trees\Feature`,
		identity.Windows,
	)
	second := identity.ForWorktree(
		`c:/repo/.git/`,
		`c:/repo trees/FEATURE/`,
		identity.Windows,
	)
	if first.ID != second.ID {
		t.Fatalf("IDs differ: %s != %s", first.ID, second.ID)
	}
}

func TestForWorktreeChangesWhenWorktreeMoves(t *testing.T) {
	first := identity.ForWorktree("/repo/.git", "/trees/feature-a", identity.Unix)
	second := identity.ForWorktree("/repo/.git", "/trees/feature-b", identity.Unix)
	if first.ID == second.ID {
		t.Fatalf("IDs should differ after move: %s", first.ID)
	}
}
