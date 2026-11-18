//go:build windows

package pxar

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func enableSparseFile(file *os.File) (bool, error) {
	var returned uint32
	err := windows.DeviceIoControl(windows.Handle(file.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &returned, nil)
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return false, nil // Preserve the payload as a dense file on this filesystem.
	}
	return err == nil, err
}
