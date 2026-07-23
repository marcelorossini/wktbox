package gateway_test

import (
	"strings"
	"testing"

	"wktbox/internal/config"
	"wktbox/internal/gateway"
)

func TestRenderUsesPerBoxHostsAndDockerUpstreams(t *testing.T) {
	got, err := gateway.Render(map[string]config.Route{
		"frontend": {Port: 5173},
		"api":      {Port: 8000},
	}, "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	for _, expected := range []string{
		"frontend.a4f8c9137d2b.localhost",
		"api.a4f8c9137d2b.localhost",
		"proxy_pass http://docker:5173",
		"proxy_pass http://docker:8000",
		"proxy_set_header Upgrade $http_upgrade",
		"proxy_set_header Connection $connection_upgrade",
		"proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for",
		"proxy_set_header X-Forwarded-Host $host",
		"proxy_set_header X-Forwarded-Proto $scheme",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("config missing %q:\n%s", expected, body)
		}
	}
}

func TestRenderIsDeterministicAndSortsRoutes(t *testing.T) {
	got, err := gateway.Render(map[string]config.Route{
		"zeta":  {Port: 9000},
		"alpha": {Port: 8000},
	}, "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if strings.Index(body, "alpha.a4f8c9137d2b.localhost") >
		strings.Index(body, "zeta.a4f8c9137d2b.localhost") {
		t.Fatalf("routes are not sorted:\n%s", body)
	}
}

func TestRenderRejectsUnsafeNamesIDsAndPorts(t *testing.T) {
	tests := []struct {
		name   string
		routes map[string]config.Route
		boxID  string
	}{
		{
			name:   "route injection",
			routes: map[string]config.Route{"api; return 200": {Port: 8000}},
			boxID:  "a4f8c9137d2b",
		},
		{
			name:   "invalid box ID",
			routes: map[string]config.Route{"api": {Port: 8000}},
			boxID:  "INVALID",
		},
		{
			name:   "invalid port",
			routes: map[string]config.Route{"api": {Port: 0}},
			boxID:  "a4f8c9137d2b",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := gateway.Render(test.routes, test.boxID); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRenderWithNoRoutesExposesOnlyDefault404Server(t *testing.T) {
	got, err := gateway.Render(nil, "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.Contains(body, "default_server") || !strings.Contains(body, "return 404") {
		t.Fatalf("default server missing:\n%s", body)
	}
	if strings.Contains(body, "proxy_pass") {
		t.Fatalf("empty routes exposed an upstream:\n%s", body)
	}
}
