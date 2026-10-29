//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
)

func hostIDs() (int, int) {
	return os.Getuid(), os.Getgid()
}

// hostTimezone returns the IANA zone configured on the host, so the Webtop
// clock and in-box tools match the developer's local time when TZ is unset.
func hostTimezone() string {
	return timezoneFromLocaltime(os.Readlink, os.ReadFile)
}

func timezoneFromLocaltime(
	readlink func(string) (string, error),
	readFile func(string) ([]byte, error),
) string {
	if target, err := readlink("/etc/localtime"); err == nil {
		if _, zone, found := strings.Cut(filepath.ToSlash(target), "zoneinfo/"); found &&
			validTimezone(zone) {
			return zone
		}
	}
	if body, err := readFile("/etc/timezone"); err == nil {
		if zone := strings.TrimSpace(string(body)); validTimezone(zone) {
			return zone
		}
	}
	return ""
}

func validTimezone(zone string) bool {
	if zone == "" || strings.HasPrefix(zone, "/") || strings.Contains(zone, "..") {
		return false
	}
	for _, character := range zone {
		if !(character == '/' || character == '_' || character == '-' || character == '+' ||
			(character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9')) {
			return false
		}
	}
	return true
}
