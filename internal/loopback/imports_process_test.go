package loopback_test

import (
	"context"
	"strings"
	"testing"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestProcessImportFactoryRejectsMissingRelayConfiguration(t *testing.T) {
	_, err := loopback.NewProcessImportFactory(loopback.ProcessImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "relay address") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunImportProxyRejectsInvalidPIDBeforeOpeningListeners(t *testing.T) {
	err := loopback.RunImportProxy(context.Background(), loopback.ImportProxyRunOptions{
		PID:          -1,
		Port:         1234,
		RelayAddress: "127.0.0.1:23004",
		Token:        make([]byte, portforward.TokenSize),
		MappingName:  "api",
	})
	if err == nil || !strings.Contains(err.Error(), "PID") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunImportProxyArgsRequiresAllFlags(t *testing.T) {
	err := loopback.RunImportProxyArgs(
		context.Background(),
		[]string{"--pid", "42"},
	)
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("error = %v", err)
	}
}
