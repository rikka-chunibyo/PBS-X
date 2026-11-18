//go:build windows

package agentfs

import (
	"os"

	"golang.org/x/sys/windows"
)

func nativeReadChange(file *os.File) ([2]int64, error) {
	var info fileNetworkOpenInformation
	if err := ntQueryFileNetworkOpenInformation(windows.Handle(file.Fd()), &info); err != nil {
		return [2]int64{}, err
	}
	return [2]int64{info.ChangeTime, info.LastWriteTime}, nil
}
