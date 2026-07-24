package prune_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/prune"
	"wktbox/internal/state"
)

func TestDiscoverReportsOnlyMissingWorktreesAsCandidates(t *testing.T) {
	existing := t.TempDir()
	missing := filepath.Join(t.TempDir(), "removed")
	records := []state.BoxRecord{
		{ID: "bbbbbbbbbbbb", Name: "existing", Worktree: existing},
		{ID: "aaaaaaaaaaaa", Name: "removed", Worktree: missing},
	}

	report := prune.Discover(records, os.Stat)

	want := []prune.Candidate{{
		ID:       "aaaaaaaaaaaa",
		Name:     "removed",
		Worktree: missing,
	}}
	if !reflect.DeepEqual(report.Candidates, want) {
		t.Fatalf("candidates = %#v, want %#v", report.Candidates, want)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %#v", report.Warnings)
	}
}

func TestDiscoverWarnsForEmptyWorktreeWithoutCallingStat(t *testing.T) {
	called := false
	report := prune.Discover(
		[]state.BoxRecord{{
			ID:   "aaaaaaaaaaaa",
			Name: "unknown",
		}},
		func(string) (fs.FileInfo, error) {
			called = true
			return nil, nil
		},
	)

	if called {
		t.Fatal("stat was called for an empty path")
	}
	if len(report.Candidates) != 0 {
		t.Fatalf("candidates = %#v", report.Candidates)
	}
	if len(report.Warnings) != 1 ||
		report.Warnings[0].ID != "aaaaaaaaaaaa" ||
		report.Warnings[0].Path != "" {
		t.Fatalf("warnings = %#v", report.Warnings)
	}
}

func TestDiscoverWarnsForPermissionAndOtherStatErrors(t *testing.T) {
	denied := &os.PathError{
		Op:   "stat",
		Path: "/private/worktree",
		Err:  fs.ErrPermission,
	}
	report := prune.Discover(
		[]state.BoxRecord{{
			ID:       "aaaaaaaaaaaa",
			Name:     "private",
			Worktree: "/private/worktree",
		}},
		func(string) (fs.FileInfo, error) {
			return nil, denied
		},
	)

	if len(report.Candidates) != 0 {
		t.Fatalf("candidates = %#v", report.Candidates)
	}
	if len(report.Warnings) != 1 ||
		!strings.Contains(report.Warnings[0].Message, "permission denied") {
		t.Fatalf("warnings = %#v", report.Warnings)
	}
}

func TestDiscoverTreatsWrappedNotExistAsMissing(t *testing.T) {
	report := prune.Discover(
		[]state.BoxRecord{{
			ID:       "aaaaaaaaaaaa",
			Name:     "removed",
			Worktree: "/removed/worktree",
		}},
		func(string) (fs.FileInfo, error) {
			return nil, errors.Join(errors.New("lookup"), fs.ErrNotExist)
		},
	)

	if len(report.Candidates) != 1 {
		t.Fatalf("candidates = %#v", report.Candidates)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings = %#v", report.Warnings)
	}
}

func TestDiscoverSortsOutputAndDeduplicatesRecords(t *testing.T) {
	records := []state.BoxRecord{
		{ID: "cccccccccccc", Name: "third", Worktree: "/removed/third"},
		{ID: "aaaaaaaaaaaa", Name: "first", Worktree: "/removed/first"},
		{ID: "bbbbbbbbbbbb", Name: "second", Worktree: "/removed/second"},
		{ID: "aaaaaaaaaaaa", Name: "first", Worktree: "/removed/first"},
	}
	report := prune.Discover(
		records,
		func(string) (fs.FileInfo, error) {
			return nil, fs.ErrNotExist
		},
	)

	var ids []string
	for _, candidate := range report.Candidates {
		ids = append(ids, candidate.ID)
	}
	if !reflect.DeepEqual(ids, []string{
		"aaaaaaaaaaaa",
		"bbbbbbbbbbbb",
		"cccccccccccc",
	}) {
		t.Fatalf("candidate IDs = %#v", ids)
	}
}
