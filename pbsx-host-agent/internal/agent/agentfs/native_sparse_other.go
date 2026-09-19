//go:build !linux && !windows

package agentfs

func nativeSparseHoles(path string) (uint64, []sparseRange, error) {
	return 0, nil, nil
}
