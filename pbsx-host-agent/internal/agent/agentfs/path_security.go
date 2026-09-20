package agentfs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// canonicalRelativePath normalizes a path received from the PXAR walker or
// legacy agentfs peer and rejects any syntax capable of escaping the configured
// snapshot root. It intentionally uses slash semantics before converting to the
// local OS so Windows drive/UNC/device paths are rejected even when tests run on
// Unix.
func canonicalRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("path contains NUL byte")
	}
	if value == "" || value == "." {
		return ".", nil
	}

	slash := strings.ReplaceAll(value, `\\`, "/")
	// PXAR and legacy agentfs peers use exactly "/" to address the configured
	// snapshot root. Other absolute paths remain forbidden.
	if slash == "/" {
		return ".", nil
	}
	lower := strings.ToLower(slash)
	if strings.HasPrefix(slash, "/") || strings.HasPrefix(slash, "//") ||
		strings.HasPrefix(lower, "//?/") || strings.HasPrefix(lower, "/??/") {
		return "", fmt.Errorf("absolute/device path is not allowed: %q", value)
	}
	if len(slash) >= 2 && ((slash[0] >= 'A' && slash[0] <= 'Z') || (slash[0] >= 'a' && slash[0] <= 'z')) && slash[1] == ':' {
		return "", fmt.Errorf("drive-qualified path is not allowed: %q", value)
	}

	parts := strings.Split(slash, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("parent traversal is not allowed: %q", value)
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return ".", nil
	}
	return filepath.FromSlash(strings.Join(clean, "/")), nil
}

func (s *Server) safeAbs(value string) (string, error) {
	rel, err := canonicalRelativePath(value)
	if err != nil {
		return "", err
	}
	full := s.abs(rel)
	if rel == "." {
		return full, nil
	}
	if err := ensureResolvedParentWithinRoot(s.snapshot.Path, full); err != nil {
		return "", err
	}
	return full, nil
}

func ensureResolvedParentWithinRoot(root, full string) error {
	root = securityFilesystemPath(root)
	full = securityFilesystemPath(full)
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootResolved, err = filepath.Abs(root)
		if err != nil {
			return err
		}
	}
	parentResolved, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return fmt.Errorf("resolve snapshot path parent: %w", err)
	}
	rel, err := filepath.Rel(rootResolved, parentResolved)
	if err != nil {
		return fmt.Errorf("compare snapshot path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes snapshot root through symlink/reparse point")
	}
	return nil
}

func securityFilesystemPath(value string) string {
	// filepath.EvalSymlinks expects ordinary Win32 paths, not the extended path
	// prefix used by agentfs for actual I/O.
	if strings.HasPrefix(value, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(value, `\\?\UNC\`)
	}
	if strings.HasPrefix(value, `\\?\`) {
		return strings.TrimPrefix(value, `\\?\`)
	}
	if strings.HasPrefix(value, `\??\`) {
		return strings.TrimPrefix(value, `\??\`)
	}
	return value
}
