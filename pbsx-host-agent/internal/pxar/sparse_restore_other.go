//go:build !linux && !windows

package pxar

import (
	"context"
	"os"
)

func applySparseLayout(ctx context.Context, st *restoreState, file *os.File, xattrs map[string][]byte, fileSize uint64) {
}
