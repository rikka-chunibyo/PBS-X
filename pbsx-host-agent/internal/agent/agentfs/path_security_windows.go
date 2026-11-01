//go:build windows

package agentfs

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

const volumeNameNT = 0x2

// resolveSecurityPath follows reparse points through a real Windows handle.
// filepath.EvalSymlinks cannot walk the GLOBALROOT device path returned by a
// VSS mount point, while GetFinalPathNameByHandle returns a stable path that
// can still be compared against the trusted snapshot root.
func resolveSecurityPath(value string) (string, error) {
	if strings.HasPrefix(value, `\??\`) {
		value = `\\?\` + strings.TrimPrefix(value, `\??\`)
	}
	ptr, err := windows.UTF16PtrFromString(value)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		ptr,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), volumeNameNT)
		if err != nil {
			return "", fmt.Errorf("get final path: %w", err)
		}
		if n < uint32(len(buffer)) {
			return windows.UTF16ToString(buffer[:n]), nil
		}
		buffer = make([]uint16, n+1)
	}
}
