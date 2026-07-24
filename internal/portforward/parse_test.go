package portforward_test

import (
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/portforward"
)

func TestParsePositionalImportsPreservesPorts(t *testing.T) {
	got, err := portforward.ParseImports([]string{"1234", "5432"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []portforward.Mapping{
		{
			Name:          "import-1234",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    1234,
			TargetPort:    1234,
		},
		{
			Name:          "import-5432",
			Direction:     portforward.Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    5432,
			TargetPort:    5432,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mappings = %#v, want %#v", got, want)
	}
}

func TestParseNamedImportAndPublicationMaps(t *testing.T) {
	imports, err := portforward.ParseImports(
		nil,
		[]string{"redis=127.0.0.1:6379:16379"},
	)
	if err != nil {
		t.Fatal(err)
	}
	publications, err := portforward.ParsePublications(
		[]string{"api=127.0.0.1:18000:8000"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 1 ||
		imports[0].Name != "redis" ||
		imports[0].SourcePort != 6379 ||
		imports[0].TargetPort != 16379 {
		t.Fatalf("imports = %#v", imports)
	}
	if len(publications) != 1 ||
		publications[0].Name != "api" ||
		publications[0].SourcePort != 18000 ||
		publications[0].TargetPort != 8000 {
		t.Fatalf("publications = %#v", publications)
	}
}

func TestParseImportsRejectsMixedSyntaxAndInvalidPorts(t *testing.T) {
	_, err := portforward.ParseImports(
		[]string{"1234"},
		[]string{"api=127.0.0.1:1234:1234"},
	)
	if err == nil || !strings.Contains(err.Error(), "either positional ports or --map") {
		t.Fatalf("mixed syntax error = %v", err)
	}

	_, err = portforward.ParseImports([]string{"0"}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("invalid port error = %v", err)
	}
}

func TestParsePublicationsRejectsNonLoopbackBind(t *testing.T) {
	_, err := portforward.ParsePublications(
		[]string{"api=0.0.0.0:18000:8000"},
	)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v", err)
	}
}
