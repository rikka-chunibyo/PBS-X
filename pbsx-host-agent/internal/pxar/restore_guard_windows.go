//go:build windows

package pxar

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows metadata and ADS need access flags unavailable through os.Root's
// public OpenFile API. Resolve the parent through Root, then use NtCreateFile
// relative to that directory handle, never an absolute pathname. Reparse-point
// traversal is forbidden even if a name changes during the operation.
func openWindowsRestoreFile(st *restoreState, p, stream string, access uint32, create, symlink bool) (*os.File, error) {
	target, closeTarget, err := st.destinationFS(p)
	if err != nil {
		return nil, err
	}
	defer closeTarget()
	rel, err := target.relative(p)
	if err != nil {
		return nil, err
	}
	parent, err := target.root.OpenRoot(filepath.Dir(rel))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	directory, err := parent.Open(".")
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	name := filepath.Base(rel)
	// NT uses an empty relative name to reopen the directory handle itself.
	// A literal dot is not a valid NT object name.
	if name == "." {
		name = ""
	}
	if stream != "" {
		name += ":" + stream
	}
	unicode, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	attrs := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(directory.Fd()), ObjectName: unicode, Attributes: windows.OBJ_CASE_INSENSITIVE}
	if !symlink {
		attrs.Attributes |= windows.OBJ_DONT_REPARSE
	}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	disposition := uint32(windows.FILE_OPEN)
	if create {
		disposition = windows.FILE_OVERWRITE_IF
	}
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES, &attrs, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition, windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("open confined restore metadata %q: %w", p, err)
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if !symlink && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("restore metadata refuses reparse point %q", p)
	}
	return os.NewFile(uintptr(handle), p), nil
}

func writeRestoreADS(st *restoreState, p, stream string, data []byte) error {
	file, err := openWindowsRestoreFile(st, p, stream, windows.GENERIC_WRITE, true, false)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
