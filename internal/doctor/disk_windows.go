//go:build windows

package doctor

import "golang.org/x/sys/windows"

func availableDiskBytes(path string) (uint64, error) {
	existing, err := nearestExistingPath(path)
	if err != nil {
		return 0, err
	}
	pathPointer, err := windows.UTF16PtrFromString(existing)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pathPointer, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}
