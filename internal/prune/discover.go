package prune

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"wktbox/internal/state"
)

type StatFunc func(string) (fs.FileInfo, error)

func Discover(records []state.BoxRecord, stat StatFunc) Report {
	if stat == nil {
		stat = os.Stat
	}
	report := Report{
		Candidates: make([]Candidate, 0),
		Destroyed:  make([]Candidate, 0),
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, duplicate := seen[record.ID]; duplicate {
			continue
		}
		seen[record.ID] = struct{}{}
		if record.Worktree == "" {
			report.Warnings = append(report.Warnings, Warning{
				ID:      record.ID,
				Path:    record.Worktree,
				Message: "recorded worktree path is empty",
			})
			continue
		}
		_, err := stat(record.Worktree)
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			report.Candidates = append(report.Candidates, Candidate{
				ID:       record.ID,
				Name:     record.Name,
				Worktree: record.Worktree,
			})
		default:
			report.Warnings = append(report.Warnings, Warning{
				ID:      record.ID,
				Path:    record.Worktree,
				Message: fmt.Sprintf("inspect recorded worktree: %v", err),
			})
		}
	}
	sort.Slice(report.Candidates, func(left, right int) bool {
		return report.Candidates[left].ID < report.Candidates[right].ID
	})
	sort.Slice(report.Warnings, func(left, right int) bool {
		if report.Warnings[left].ID == report.Warnings[right].ID {
			return report.Warnings[left].Path < report.Warnings[right].Path
		}
		return report.Warnings[left].ID < report.Warnings[right].ID
	})
	return report
}
