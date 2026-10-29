//go:build !windows

package app

import (
	"errors"
	"testing"
)

func TestTimezoneFromLocaltime(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("missing") }
	noFile := func(string) ([]byte, error) { return nil, errors.New("missing") }
	for name, test := range map[string]struct {
		readlink func(string) (string, error)
		readFile func(string) ([]byte, error)
		want     string
	}{
		"symlink": {
			readlink: func(string) (string, error) {
				return "/usr/share/zoneinfo/America/Sao_Paulo", nil
			},
			readFile: noFile,
			want:     "America/Sao_Paulo",
		},
		"relative symlink": {
			readlink: func(string) (string, error) {
				return "../usr/share/zoneinfo/Etc/UTC", nil
			},
			readFile: noFile,
			want:     "Etc/UTC",
		},
		"timezone file": {
			readlink: missing,
			readFile: func(string) ([]byte, error) { return []byte("Europe/Lisbon\n"), nil },
			want:     "Europe/Lisbon",
		},
		"unsafe value": {
			readlink: missing,
			readFile: func(string) ([]byte, error) { return []byte("x\nTZ=evil"), nil },
			want:     "",
		},
		"unknown": {readlink: missing, readFile: noFile, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := timezoneFromLocaltime(test.readlink, test.readFile); got != test.want {
				t.Fatalf("timezone = %q, want %q", got, test.want)
			}
		})
	}
}
