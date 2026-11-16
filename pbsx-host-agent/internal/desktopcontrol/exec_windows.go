//go:build windows

package desktopcontrol

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func protectEnrollmentDirectory(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

type shellExecuteInfo struct {
	Size                              uint32
	Mask                              uint32
	Window                            windows.Handle
	Verb, File, Parameters, Directory *uint16
	Show                              int32
	Instance                          windows.Handle
	IDList                            uintptr
	Class                             *uint16
	ClassKey                          windows.Handle
	HotKey                            uint32
	Icon                              windows.Handle
	Process                           windows.Handle
}

func execute(ctx context.Context, args []string, elevate bool) error {
	programFiles, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return fmt.Errorf("resolve Program Files: %w", err)
	}
	agentPath := filepath.Join(programFiles, "PBS-X", "Agent", "pbsx-agent.exe")
	if windows.GetCurrentProcessToken().IsElevated() {
		out, err := exec.CommandContext(ctx, agentPath, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("agent operation failed (%v): %s", err, out)
		}
		return nil
	}
	if !elevate {
		return fmt.Errorf("open an Administrator terminal for this operation")
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = syscall.EscapeArg(arg)
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(agentPath)
	if err != nil {
		return err
	}
	parameters, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return err
	}
	info := shellExecuteInfo{Mask: 0x40 | 0x100, Verb: verb, File: file, Parameters: parameters, Show: 0}
	info.Size = uint32(unsafe.Sizeof(info))
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	result, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return fmt.Errorf("administrator approval failed: %w", callErr)
	}
	if info.Process == 0 {
		return fmt.Errorf("agent process handle unavailable")
	}
	defer windows.CloseHandle(info.Process)
	for {
		result, err := windows.WaitForSingleObject(info.Process, 100)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			var code uint32
			if err := windows.GetExitCodeProcess(info.Process, &code); err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("agent operation failed (exit %d); retry with pbsx-agentctl in an Administrator terminal for details", code)
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
