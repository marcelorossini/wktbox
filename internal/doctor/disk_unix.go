//go:build !windows

package doctor

import "syscall"

func availableDiskBytes(path string) (uint64, error) {
	existing, err := nearestExistingPath(path)
	if err != nil {
		return 0, err
	}
	var statistics syscall.Statfs_t
	if err := syscall.Statfs(existing, &statistics); err != nil {
		return 0, err
	}
	return statistics.Bavail * uint64(statistics.Bsize), nil
}
