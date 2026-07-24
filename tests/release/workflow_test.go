package release_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestWorkflowTriggerAndVersionValidation(t *testing.T) {
	workflow, raw := readWorkflow(t)
	on := object(t, workflow["on"], "on")
	push := object(t, on["push"], "on.push")
	tags := stringsOf(t, push["tags"], "on.push.tags")
	if !equalStrings(tags, []string{"v*.*.*"}) {
		t.Fatalf("release tags = %#v", tags)
	}
	validate := job(t, workflow, "validate")
	run := jobRun(validate)
	for _, expected := range []string{
		`tag="${GITHUB_REF_NAME}"`,
		`version="${tag#v}"`,
		`test "$tag" = "v$version"`,
		`^v[0-9]+\.[0-9]+\.[0-9]+`,
		`-X wktbox/internal/version.value=$version`,
		`"$RUNNER_TEMP/wktbox" --version`,
		`GITHUB_OUTPUT`,
	} {
		if !strings.Contains(run, expected) {
			t.Errorf("validate job missing %q", expected)
		}
	}
	if strings.Contains(raw, "internal/version/version.go") {
		t.Error("release workflow must not edit tracked version source")
	}
}

func TestWorkflowReleaseDependsOnEveryPublicationGate(t *testing.T) {
	workflow, _ := readWorkflow(t)
	for _, name := range []string{
		"validate",
		"verify",
		"artifacts",
		"webtop-image",
		"gateway-image",
		"promote-latest",
		"release",
	} {
		_ = job(t, workflow, name)
	}
	releaseNeeds := jobNeeds(t, job(t, workflow, "release"))
	wantReleaseNeeds := []string{
		"artifacts",
		"gateway-image",
		"promote-latest",
		"validate",
		"verify",
		"webtop-image",
	}
	sort.Strings(releaseNeeds)
	if !equalStrings(releaseNeeds, wantReleaseNeeds) {
		t.Fatalf("release needs = %#v, want %#v", releaseNeeds, wantReleaseNeeds)
	}
	promotionNeeds := jobNeeds(t, job(t, workflow, "promote-latest"))
	sort.Strings(promotionNeeds)
	if !equalStrings(
		promotionNeeds,
		[]string{"gateway-image", "webtop-image"},
	) {
		t.Fatalf("promote-latest needs = %#v", promotionNeeds)
	}

	jobs := object(t, workflow["jobs"], "jobs")
	for name, value := range jobs {
		run := jobRun(object(t, value, "jobs."+name))
		hasRelease := strings.Contains(run, "gh release create")
		if name == "release" && !hasRelease {
			t.Error("final release job does not create the GitHub Release")
		}
		if name != "release" && hasRelease {
			t.Errorf("job %s creates the GitHub Release early", name)
		}
	}
}

func TestWorkflowAttestsArtifactsAndImagesWithLeastPrivilege(t *testing.T) {
	workflow, _ := readWorkflow(t)
	global := permissions(t, workflow)
	if len(global) != 1 || global["contents"] != "read" {
		t.Fatalf("global permissions = %#v", global)
	}
	for _, name := range []string{
		"artifacts",
		"webtop-image",
		"gateway-image",
	} {
		current := job(t, workflow, name)
		if !strings.Contains(
			jobUses(current),
			"actions/attest-build-provenance@",
		) {
			t.Errorf("job %s does not attest provenance", name)
		}
		got := permissions(t, current)
		if got["id-token"] != "write" ||
			got["attestations"] != "write" ||
			got["artifact-metadata"] != "write" ||
			got["contents"] != "read" {
			t.Errorf("job %s permissions = %#v", name, got)
		}
	}

	jobs := object(t, workflow["jobs"], "jobs")
	for name, value := range jobs {
		got := permissions(t, object(t, value, "jobs."+name))
		if got["contents"] == "write" && name != "release" {
			t.Errorf("contents: write leaked to %s", name)
		}
		if got["packages"] == "write" &&
			name != "webtop-image" &&
			name != "gateway-image" &&
			name != "promote-latest" {
			t.Errorf("packages: write leaked to %s", name)
		}
		if got["id-token"] == "write" &&
			name != "artifacts" &&
			name != "webtop-image" &&
			name != "gateway-image" {
			t.Errorf("id-token: write leaked to %s", name)
		}
		if got["attestations"] == "write" &&
			name != "artifacts" &&
			name != "webtop-image" &&
			name != "gateway-image" {
			t.Errorf("attestations: write leaked to %s", name)
		}
		if got["artifact-metadata"] == "write" &&
			name != "artifacts" &&
			name != "webtop-image" &&
			name != "gateway-image" {
			t.Errorf("artifact-metadata: write leaked to %s", name)
		}
	}
	if permissions(t, job(t, workflow, "release"))["contents"] != "write" {
		t.Error("final release job lacks contents: write")
	}
}

func TestWorkflowBuildsImmutableMultiArchImagesAndPromotesLatestLast(t *testing.T) {
	workflow, raw := readWorkflow(t)
	for _, name := range []string{"webtop-image", "gateway-image"} {
		run := jobRun(job(t, workflow, name))
		for _, expected := range []string{
			"linux/amd64,linux/arm64",
			"--build-arg VERSION=",
			"--build-arg REVISION=",
			"--build-arg SOURCE_URL=",
			"push-by-digest=true",
			"containerimage.digest",
			"imagetools inspect",
			"head -n 1 || true",
			"digest mismatch",
			"imagetools create",
		} {
			if !strings.Contains(run, expected) {
				t.Errorf("%s job missing %q", name, expected)
			}
		}
		if strings.Contains(run, ":latest") {
			t.Errorf("%s moves latest before the other image succeeds", name)
		}
	}
	promote := jobRun(job(t, workflow, "promote-latest"))
	for _, expected := range []string{
		"wktbox/webtop:latest",
		"wktbox/gateway:latest",
		"needs.webtop-image.outputs.digest",
		"needs.gateway-image.outputs.digest",
	} {
		if !strings.Contains(promote, expected) {
			t.Errorf("latest promotion missing %q", expected)
		}
	}
	for _, expected := range []string{
		"wktbox/webtop:${{ needs.validate.outputs.version }}",
		"wktbox/gateway:${{ needs.validate.outputs.version }}",
	} {
		if !strings.Contains(raw, expected) {
			t.Errorf("versioned image reference missing %q", expected)
		}
	}
}

func TestWorkflowPinsEveryActionToACommit(t *testing.T) {
	_, raw := readWorkflow(t)
	uses := regexp.MustCompile(`(?m)^\s*-\s+uses:\s+([^\s]+)`).FindAllStringSubmatch(
		raw,
		-1,
	)
	if len(uses) == 0 {
		t.Fatal("release workflow has no Actions")
	}
	pinned := regexp.MustCompile(
		`^[^@]+@[0-9a-f]{40}$`,
	)
	for _, match := range uses {
		if !pinned.MatchString(match[1]) {
			t.Errorf("Action is not SHA-pinned: %s", match[1])
		}
	}
}

func readWorkflow(t *testing.T) (map[string]any, string) {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate workflow test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	content, err := os.ReadFile(
		filepath.Join(root, ".github", "workflows", "release.yml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow, string(content)
}

func job(t *testing.T, workflow map[string]any, name string) map[string]any {
	t.Helper()
	jobs := object(t, workflow["jobs"], "jobs")
	value, exists := jobs[name]
	if !exists {
		t.Fatalf("workflow job %q does not exist", name)
	}
	return object(t, value, "jobs."+name)
}

func jobNeeds(t *testing.T, current map[string]any) []string {
	t.Helper()
	value, exists := current["needs"]
	if !exists {
		return nil
	}
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		return stringsOf(t, typed, "needs")
	default:
		t.Fatalf("needs has type %T", value)
		return nil
	}
}

func jobRun(current map[string]any) string {
	steps, _ := current["steps"].([]any)
	var result strings.Builder
	for _, value := range steps {
		step, _ := value.(map[string]any)
		if run, ok := step["run"].(string); ok {
			result.WriteString(run)
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func jobUses(current map[string]any) string {
	steps, _ := current["steps"].([]any)
	var result strings.Builder
	for _, value := range steps {
		step, _ := value.(map[string]any)
		if uses, ok := step["uses"].(string); ok {
			result.WriteString(uses)
			result.WriteByte('\n')
		}
	}
	return result.String()
}

func permissions(t *testing.T, value map[string]any) map[string]string {
	t.Helper()
	raw, exists := value["permissions"]
	if !exists {
		return map[string]string{}
	}
	current := object(t, raw, "permissions")
	result := make(map[string]string, len(current))
	for key, value := range current {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("permission %s has type %T", key, value)
		}
		result[key] = text
	}
	return result
}

func object(t *testing.T, value any, path string) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s has type %T", path, value)
	}
	return result
}

func stringsOf(t *testing.T, value any, path string) []string {
	t.Helper()
	values, ok := value.([]any)
	if !ok {
		t.Fatalf("%s has type %T", path, value)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("%s item has type %T", path, value)
		}
		result = append(result, text)
	}
	return result
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
