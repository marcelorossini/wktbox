package portforward_test

import (
	"bytes"
	"strings"
	"testing"

	"wktbox/internal/portforward"
)

func TestRelayPreambleRoundTrips(t *testing.T) {
	token := bytes.Repeat([]byte{0x42}, portforward.TokenSize)
	var encoded bytes.Buffer
	if err := portforward.WritePreamble(&encoded, token, "postgres"); err != nil {
		t.Fatal(err)
	}

	gotToken, gotName, err := portforward.ReadPreamble(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotToken, token) || gotName != "postgres" {
		t.Fatalf("token=%x name=%q", gotToken, gotName)
	}
}

func TestRelayPreambleRejectsInvalidTokenAndLongName(t *testing.T) {
	var encoded bytes.Buffer
	err := portforward.WritePreamble(
		&encoded,
		[]byte("short"),
		"postgres",
	)
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("token error = %v", err)
	}

	err = portforward.WritePreamble(
		&encoded,
		bytes.Repeat([]byte{0x42}, portforward.TokenSize),
		strings.Repeat("a", 64),
	)
	if err == nil || !strings.Contains(err.Error(), "63") {
		t.Fatalf("name error = %v", err)
	}
}
