package agents_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSkillFrontmatterUsesOnlyPortableFields(t *testing.T) {
	content := readSkill(t)
	frontmatter, _ := splitFrontmatter(t, content)
	got := make(map[string]string)
	for _, line := range strings.Split(frontmatter, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			t.Fatalf("invalid frontmatter line %q", line)
		}
		got[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	want := map[string]string{
		"name": "wktbox-isolated-development",
		"description": "Use Wktbox for isolated Docker development in a " +
			"current checkout or linked Git worktree.",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frontmatter = %#v, want %#v", got, want)
	}
}

func TestSkillContainsOrderedAutonomousWorkflow(t *testing.T) {
	_, body := splitFrontmatter(t, readSkill(t))
	body = normalizeWhitespace(body)
	required := []string{
		"repository itself is Wktbox",
		"stay on the host",
		"explicit isolation signals",
		"Ask the user when isolation intent is unclear",
		"current checkout by default",
		"linked worktree only",
		"wktbox doctor --path <checkout>",
		"Stop on doctor failure",
		"wktbox run --path <checkout> --",
		"wktbox compose --path <checkout> --",
		"wktbox open --path <checkout>",
		"Keep current-checkout boxes until explicit user cleanup",
		"Keep linked-worktree boxes through pull-request review",
		"confirming merge into `main`",
		"wktbox destroy --path <checkout> --force",
		"git worktree remove <checkout>",
		"Use `wktbox prune` only for discovery",
		"`wktbox prune --force` only when deletion is explicitly authorized",
	}
	previous := -1
	for _, phrase := range required {
		index := strings.Index(body, phrase)
		if index < 0 {
			t.Errorf("skill body missing %q", phrase)
			continue
		}
		if index < previous {
			t.Errorf("skill phrase is out of order: %q", phrase)
		}
		previous = index
	}
}

func TestSkillMakesWorktreesOptionalAndForbidsDoctorFallback(t *testing.T) {
	_, body := splitFrontmatter(t, readSkill(t))
	body = normalizeWhitespace(body)
	for _, phrase := range []string{
		"Do not require a new worktree",
		"Do not initialize the box",
		"do not fall back to host execution",
		"Opening a pull request is not cleanup authorization",
		"Never run the Wktbox repository inside Wktbox",
	} {
		if !strings.Contains(body, phrase) {
			t.Errorf("skill body missing policy %q", phrase)
		}
	}
	doctor := strings.Index(body, "wktbox doctor --path <checkout>")
	initialization := strings.Index(body, "wktbox run --path <checkout> --")
	if doctor < 0 || initialization < 0 || doctor >= initialization {
		t.Fatalf(
			"doctor must precede initialization: doctor=%d run=%d",
			doctor,
			initialization,
		)
	}
	destroy := strings.Index(body, "wktbox destroy --path <checkout> --force")
	remove := strings.Index(body, "git worktree remove <checkout>")
	if destroy < 0 || remove < 0 || destroy >= remove {
		t.Fatalf(
			"destroy must precede worktree removal: destroy=%d remove=%d",
			destroy,
			remove,
		)
	}
}

func readSkill(t *testing.T) string {
	t.Helper()
	path := filepath.Join(
		"..",
		"..",
		"internal",
		"agentintegration",
		"assets",
		"wktbox-isolated-development",
		"SKILL.md",
	)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func splitFrontmatter(t *testing.T, content string) (string, string) {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("skill must begin with YAML frontmatter")
	}
	remainder := strings.TrimPrefix(content, "---\n")
	frontmatter, body, found := strings.Cut(remainder, "\n---\n")
	if !found {
		t.Fatal("skill frontmatter is not closed")
	}
	return frontmatter, body
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
