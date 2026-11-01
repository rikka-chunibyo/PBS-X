//go:build !windows

package agentfs

import "path/filepath"

func resolveSecurityPath(value string) (string, error) {
	return filepath.EvalSymlinks(securityFilesystemPath(value))
}
