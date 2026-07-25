//go:build linux

package loopback

import (
	"context"
	"strings"
	"testing"
)

func TestDialNamespaceRejectsInvalidPID(t *testing.T) {
	connection, err := dialNamespaceContext(
		context.Background(),
		0,
		"tcp",
		"127.0.0.1:5174",
	)
	if connection != nil {
		connection.Close()
		t.Fatal("connection was created for invalid PID")
	}
	if err == nil || !strings.Contains(err.Error(), "PID must be positive") {
		t.Fatalf("error = %v", err)
	}
}

func TestDialNamespaceRejectsHostnameBeforeSetns(t *testing.T) {
	connection, err := dialNamespaceContext(
		context.Background(),
		1,
		"tcp",
		"docker:5174",
	)
	if connection != nil {
		connection.Close()
		t.Fatal("connection was created for non-numeric host")
	}
	if err == nil || !strings.Contains(err.Error(), "numeric IP address") {
		t.Fatalf("error = %v", err)
	}
}
