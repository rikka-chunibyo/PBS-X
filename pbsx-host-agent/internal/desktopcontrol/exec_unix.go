//go:build unix

package desktopcontrol

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const AgentPath = "/opt/pbsx-agent/pbsx-agent"

func protectEnrollmentDirectory(path string) error { return os.Chmod(path, 0700) }

func execute(ctx context.Context, args []string, elevate bool) error {
	for path := AgentPath; path != "/"; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("install the PBS-X agent first: %w", err)
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("agent executable and its parent directories must be root-owned and not writable by other users")
		}
		if path == AgentPath && !info.Mode().IsRegular() {
			return fmt.Errorf("invalid agent executable")
		}
	}
	var command *exec.Cmd
	if os.Geteuid() == 0 {
		command = exec.CommandContext(ctx, AgentPath, args...)
	} else if elevate {
		command = exec.CommandContext(ctx, "pkexec", append([]string{"--disable-internal-agent", AgentPath}, args...)...)
	} else {
		return fmt.Errorf("this operation requires sudo; status does not require root")
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("agent operation failed (%v): %s", err, string(output))
	}
	return nil
}
