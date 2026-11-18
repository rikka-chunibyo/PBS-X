//go:build linux

package pxar

import "os"

// Seeking over verified zeros in a fresh file preserves its logical bytes on
// filesystems both with and without sparse allocation support.
func enableSparseFile(*os.File) (bool, error) { return true, nil }
