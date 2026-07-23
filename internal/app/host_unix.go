//go:build !windows

package app

import "os"

func hostIDs() (int, int) {
	return os.Getuid(), os.Getgid()
}
