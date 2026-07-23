package redact_test

import (
	"testing"

	"wktbox/internal/redact"
)

func TestRedactorRemovesSecretValues(t *testing.T) {
	got := redact.New("s3cr3t").String("TOKEN=s3cr3t")
	if got != "TOKEN=***" {
		t.Fatalf("redacted = %q", got)
	}
}

func TestRedactorReplacesLongestValuesFirst(t *testing.T) {
	got := redact.New("token", "token-with-suffix").String(
		"short=token long=token-with-suffix",
	)
	if got != "short=*** long=***" {
		t.Fatalf("redacted = %q", got)
	}
}

func TestRedactorIgnoresEmptyValues(t *testing.T) {
	got := redact.New("", "secret").String("visible secret")
	if got != "visible ***" {
		t.Fatalf("redacted = %q", got)
	}
}
