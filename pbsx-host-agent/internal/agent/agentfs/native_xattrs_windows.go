//go:build windows

package agentfs

import "github.com/pbs-plus/pxar/format"

// NTFS metadata that has no native PXAR field is supplied by GetXAttrs via
// the canonical PBS-X metadata keys. Windows has no POSIX xattr namespace to
// enumerate here.
func nativePlatformXAttrs(string) ([]format.XAttr, error) { return nil, nil }
func nativePlatformFCaps(string) ([]byte, error)          { return nil, nil }
