//go:build windows

package agentfs

import (
	"fmt"

	"github.com/pbs-plus/pxar/backupproxy"
	"golang.org/x/sys/windows"
)

func nativeFileIdentity(path string) (backupproxy.FileIdentity, error) {
	h, err := openForAttrs(path)
	if err != nil {
		return backupproxy.FileIdentity{}, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return backupproxy.FileIdentity{}, err
	}
	return backupproxy.FileIdentity{
		Key:   fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow),
		Links: uint64(info.NumberOfLinks),
	}, nil
}
