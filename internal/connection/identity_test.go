package connection_test

import (
	"testing"

	"wktbox/internal/connection"
)

func TestIDIsStableForSortedUniqueMembers(t *testing.T) {
	first, err := connection.ID([]string{
		"dfe31c662a91",
		"a4f8c9137d2b",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := connection.ID([]string{
		"a4f8c9137d2b",
		"dfe31c662a91",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 12 {
		t.Fatalf("ids = %q and %q", first, second)
	}
}

func TestIDRejectsDuplicateOrInvalidMembers(t *testing.T) {
	tests := map[string][]string{
		"one member": {"a4f8c9137d2b"},
		"duplicate":  {"a4f8c9137d2b", "a4f8c9137d2b"},
		"invalid":    {"a4f8c9137d2b", "../other"},
	}
	for name, members := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := connection.ID(members); err == nil {
				t.Fatalf("ID(%#v) unexpectedly succeeded", members)
			}
		})
	}
}

func TestAliasUsesFullBoxID(t *testing.T) {
	if got := connection.Alias("a4f8c9137d2b"); got != "a4f8c9137d2b.wktbox" {
		t.Fatalf("alias = %q", got)
	}
}
