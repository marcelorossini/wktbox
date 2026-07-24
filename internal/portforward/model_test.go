package portforward_test

import (
	"strings"
	"testing"

	"wktbox/internal/portforward"
)

func TestValidateSetRejectsDuplicateNames(t *testing.T) {
	mappings := []portforward.Mapping{
		{
			Name:          "api",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    3000,
			TargetPort:    3000,
		},
		{
			Name:          "api",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    8000,
		},
	}
	err := portforward.ValidateSet(mappings)
	if err == nil || !strings.Contains(err.Error(), `duplicate mapping name "api"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSetRejectsDuplicatePublicationListener(t *testing.T) {
	mappings := []portforward.Mapping{
		{
			Name:          "api",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    8000,
		},
		{
			Name:          "web",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    5173,
		},
	}
	err := portforward.ValidateSet(mappings)
	if err == nil || !strings.Contains(err.Error(), "duplicate publication listener") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateSetRejectsDuplicateImportTarget(t *testing.T) {
	mappings := []portforward.Mapping{
		{
			Name:          "first",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    1234,
			TargetPort:    4321,
		},
		{
			Name:          "second",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    5678,
			TargetPort:    4321,
		},
	}
	err := portforward.ValidateSet(mappings)
	if err == nil || !strings.Contains(err.Error(), "duplicate import target") {
		t.Fatalf("error = %v", err)
	}
}
