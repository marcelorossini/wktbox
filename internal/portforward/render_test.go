package portforward_test

import (
	"encoding/json"
	"strings"
	"testing"

	"wktbox/internal/portforward"
)

func TestRenderConfigSortsMappingsAndEndsWithNewline(t *testing.T) {
	body, err := portforward.RenderConfig([]portforward.Mapping{
		{
			Name:          "web",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    15173,
			TargetPort:    5173,
		},
		{
			Name:          "postgres",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    5432,
			TargetPort:    5432,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Fatalf("config has no trailing newline: %q", body)
	}
	var decoded portforward.Config
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Mappings) != 2 ||
		decoded.Mappings[0].Name != "postgres" ||
		decoded.Mappings[1].Name != "web" {
		t.Fatalf("config = %#v", decoded)
	}
}

func TestRenderComposeOverridePublishesOnlyExplicitLoopbackMappings(t *testing.T) {
	body, err := portforward.RenderComposeOverride([]portforward.Mapping{
		{
			Name:          "api",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    8000,
		},
		{
			Name:          "postgres",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    5432,
			TargetPort:    5432,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "127.0.0.1:18000:18000") {
		t.Fatalf("publication missing:\n%s", text)
	}
	if strings.Contains(text, "5432:5432") {
		t.Fatalf("import leaked into Compose ports:\n%s", text)
	}
}

func TestRenderComposeOverrideReturnsNilWithoutPublications(t *testing.T) {
	body, err := portforward.RenderComposeOverride([]portforward.Mapping{{
		Name:          "postgres",
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    5432,
		TargetPort:    5432,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("override = %q", body)
	}
}
