package version_test

import (
	"testing"

	"wktbox/internal/version"
)

func TestStringDefaultsToDevelopment(t *testing.T) {
	if got := version.String(); got != "dev" {
		t.Fatalf("String() = %q, want dev", got)
	}
}
