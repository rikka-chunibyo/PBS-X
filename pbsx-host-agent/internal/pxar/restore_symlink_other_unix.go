//go:build unix && !linux

package pxar

import "fmt"

func restoreSymlinkXattr(_ *restoreState, p, name string, _ []byte) error {
	return fmt.Errorf("confined symlink xattr restore unsupported: %s (%s)", p, name)
}
