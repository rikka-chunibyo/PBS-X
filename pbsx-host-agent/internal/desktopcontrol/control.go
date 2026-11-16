// Package desktopcontrol shares the desktop and headless CLI's local operations.
package desktopcontrol

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Controller struct{ Elevate bool }

func ValidAction(action string) bool {
	switch action {
	case "start", "stop", "restart", "install", "uninstall":
		return true
	}
	return false
}

func (c Controller) Service(ctx context.Context, action string) error {
	if !ValidAction(action) {
		return fmt.Errorf("unsupported service action")
	}
	return execute(ctx, []string{action}, c.Elevate)
}

func (c Controller) Enroll(ctx context.Context, code string) error {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "pbsx1.") || len(code) > 64<<10 {
		return fmt.Errorf("paste a valid PBS-X enrollment code")
	}
	directory, err := os.MkdirTemp("", "pbsx-enrollment-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	if err := protectEnrollmentDirectory(directory); err != nil {
		return err
	}
	path := filepath.Join(directory, "code")
	if err := os.WriteFile(path, []byte(code), 0600); err != nil {
		return err
	}
	// Never put the enrollment secret in process arguments, environment, logs,
	// preferences or desktop status. The privileged reader does not remove files.
	return execute(ctx, []string{"setup", "--code-file", path}, c.Elevate)
}
