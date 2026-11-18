// Package pbsverify exposes the shared plaintext snapshot verifier to desktop
// backup recovery and restore callers.
package pbsverify

import "github.com/pbs-plus/pxar/datastore"

type FileSource = datastore.SnapshotFileSource
type RootIndexes = datastore.VerifiedRootIndexes
type ChunkSource = datastore.VerifiedChunkSource

func LoadRootIndexes(source FileSource, kind, id string, stamp int64) (*RootIndexes, error) {
	return datastore.LoadPlainRootIndexes(source, kind, id, stamp)
}
