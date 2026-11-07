package pxar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/fxamacker/cbor/v2"

	pxar "github.com/pbs-plus/pxar"
	"github.com/pbs-x/pbsx-host-agent/internal/log"
)

type RestoreMode int

const (
	RestoreModeNormal RestoreMode = iota
	RestoreModeZip
	RestoreModeNoAttr
)

type RestoreOptions struct {
	Mode       RestoreMode
	DestDir    string
	OnProgress func(RestoreProgress)
}

// RestoreProgress is logical restore progress reported after successfully processing files.
type RestoreProgress struct {
	Files uint64
	Bytes uint64
}

// reportErr sends a metadata error to the server immediately (never batched
// or deferred). Metadata errors are non-fatal — content is already in place —
func (st *restoreState) reportErr(ctx context.Context, op, path string, err error) {
	if err == nil {
		return
	}
	e := fmt.Errorf("%s %q: %w", op, path, err)
	if serr := st.client.SendError(ctx, e); serr != nil {
		log.Error(e, "", "sendErr", serr.Error(), "op", op, "restore", "error-report-failed")

	}
}

// runJobRecovered wraps processJob with panic recovery. An unrecovered panic
// in a goroutine terminates the process, leaking temp files and losing queued
// errors. Recovering here converts the panic into a reported error so Done
// always fires and cleanup runs.
func (st *restoreState) runJobRecovered(ctx context.Context, client *Client, job restoreJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic restoring %q: %v\n%s", job.dest, r, debug.Stack())
			log.Error(err, "", "dest", job.dest, "restore", "panic")

		}
	}()
	return processJob(ctx, st, job)
}

// forward-slash notation as the hardlink registry key so LinkTarget matches
type restoreJob struct {
	dest    string
	srcPath string
	info    pxar.FileInfo
}

type restoreState struct {
	client         *Client
	target         *restoreFS
	fsCap          filesystemCapabilities
	noAttr         bool
	hl             *hardlinkIndex
	jobs           chan<- restoreJob
	wg             *sync.WaitGroup
	pendingDirMeta []dirMeta
	pendingMu      sync.Mutex
	progressMu     sync.Mutex
	progress       RestoreProgress
	onProgress     func(RestoreProgress)
}

func (st *restoreState) recordProgress(job restoreJob) {
	if job.info.FileType != pxar.FileTypeFile && job.info.FileType != pxar.FileTypeHardlink {
		return
	}
	st.progressMu.Lock()
	st.progress.Files++
	st.progress.Bytes += job.info.RawSize
	progress := st.progress
	callback := st.onProgress
	st.progressMu.Unlock()
	if callback != nil {
		callback(progress)
	}
}

type dirMeta struct {
	dest string
	info pxar.FileInfo
}

const unixSecsMax = 32503680000

const (
	XAttrOwner          = "user.owner"
	XAttrGroup          = "user.group"
	XAttrACLs           = "user.acls"
	XAttrDACLProtected  = "user.pbsx.dacl.protected"
	XAttrFileAttributes = "user.fileattributes"
	XAttrCreationTime   = "user.creationtime"
	XAttrLastAccessTime = "user.lastaccesstime"
	XAttrLastWriteTime  = "user.lastwritetime"
	XAttrADSManifest    = "user.pbsx.ads.manifest"
	XAttrADSPrefix      = "user.pbsx.ads."
	XAttrSparseManifest = "user.pbsx.sparse.manifest"
	XAttrSparsePrefix   = "user.pbsx.sparse."
)

func IsCanonicalXAttr(name string) bool {
	switch name {
	case XAttrOwner, XAttrGroup, XAttrACLs, XAttrDACLProtected, XAttrFileAttributes,
		XAttrCreationTime, XAttrLastAccessTime, XAttrLastWriteTime, XAttrADSManifest, XAttrSparseManifest:
		return true
	}
	return strings.HasPrefix(name, XAttrADSPrefix) || strings.HasPrefix(name, XAttrSparsePrefix)
}

type sparseHole struct {
	Offset uint64 `cbor:"o"`
	Length uint64 `cbor:"l"`
}

type sparseRestoreManifest struct {
	Size   uint64 `cbor:"size"`
	Digest string `cbor:"digest"`
	Chunks uint32 `cbor:"chunks"`
}

func decodeSparseHoles(xattrs map[string][]byte, fileSize uint64) ([]sparseHole, bool, error) {
	rawManifest, ok := xattrs[XAttrSparseManifest]
	if !ok {
		return nil, false, nil
	}
	var manifest sparseRestoreManifest
	if err := cbor.Unmarshal(rawManifest, &manifest); err != nil {
		return nil, true, fmt.Errorf("decode sparse manifest: %w", err)
	}
	if manifest.Size != fileSize {
		return nil, true, fmt.Errorf("sparse manifest size %d does not match file size %d", manifest.Size, fileSize)
	}
	if manifest.Chunks == 0 || manifest.Chunks > 1<<20 {
		return nil, true, fmt.Errorf("invalid sparse metadata chunk count %d", manifest.Chunks)
	}
	digestBytes, err := hex.DecodeString(manifest.Digest)
	if err != nil || len(digestBytes) != sha256.Size {
		return nil, true, fmt.Errorf("invalid sparse metadata digest")
	}
	digestTag := manifest.Digest[:16]
	var encoded []byte
	for i := uint32(0); i < manifest.Chunks; i++ {
		key := fmt.Sprintf("%s%s.%06x", XAttrSparsePrefix, digestTag, i)
		chunk, exists := xattrs[key]
		if !exists {
			return nil, true, fmt.Errorf("missing sparse metadata chunk %d", i)
		}
		encoded = append(encoded, chunk...)
	}
	actual := sha256.Sum256(encoded)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), manifest.Digest) {
		return nil, true, fmt.Errorf("sparse metadata digest mismatch")
	}
	var holes []sparseHole
	if err := cbor.Unmarshal(encoded, &holes); err != nil {
		return nil, true, fmt.Errorf("decode sparse ranges: %w", err)
	}
	var lastEnd uint64
	for i, hole := range holes {
		if hole.Length == 0 || hole.Offset > fileSize || hole.Length > fileSize-hole.Offset {
			return nil, true, fmt.Errorf("invalid sparse range %d", i)
		}
		if i > 0 && hole.Offset < lastEnd {
			return nil, true, fmt.Errorf("overlapping sparse range %d", i)
		}
		lastEnd = hole.Offset + hole.Length
	}
	return holes, true, nil
}

func ParseXattrUnixSecs(data []byte) (secs int64, ok bool) {
	if len(data) == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil || v < 0 || v > unixSecsMax {
		return 0, false
	}
	return v, true
}

// aclFlavor discriminates POSIX vs Windows encoding of a user.acls payload
// so a cross-platform restore never applies a foreign ACL type.
type aclFlavor int

const (
	aclNone aclFlavor = iota
	aclPosix
	aclWindows
)

// detectACLFlavor probes the field discriminator ("sid" vs "tag") in a
// user.acls blob to report POSIX, Windows, or neither. cbor ignores unknown
// fields, so a foreign payload leaves its discriminator empty.
func detectACLFlavor(data []byte) aclFlavor {
	if len(data) == 0 {
		return aclNone
	}
	var probe []struct {
		SID string `cbor:"sid"`
		Tag string `cbor:"tag"`
	}
	if cbor.Unmarshal(data, &probe) != nil {
		return aclNone
	}
	var hasPosix, hasWindows bool
	for _, p := range probe {
		if p.SID != "" {
			hasWindows = true
		}
		if p.Tag != "" {
			hasPosix = true
		}
	}
	switch {
	case hasPosix && !hasWindows:
		return aclPosix
	case hasWindows && !hasPosix:
		return aclWindows
	default:
		return aclNone
	}
}

func RestoreWithOptions(ctx context.Context, client *Client, sources []string, opts RestoreOptions) error {
	if err := os.MkdirAll(opts.DestDir, 0o755); err != nil {
		return fmt.Errorf("mkdir root: %w", err)
	}

	if opts.Mode == RestoreModeZip {
		return restoreAsZips(ctx, client, sources, opts)
	}

	noAttr := opts.Mode == RestoreModeNoAttr
	return restoreNormal(ctx, client, sources, opts, noAttr)
}

func restoreNormal(ctx context.Context, client *Client, sources []string, opts RestoreOptions, noAttr bool) error {
	target, err := openRestoreFS(opts.DestDir)
	if err != nil {
		return err
	}
	defer target.root.Close()
	destDir := target.base

	prepareRestoreProcess()

	fsCap := getFilesystemCapabilities(destDir)

	numCPU := runtime.NumCPU()
	numWorkers := numCPU * 2
	if fsCap.prefersSequentialOps {
		numWorkers = min(numCPU, 2)
	}

	jobs := make(chan restoreJob, 1024)
	var wg sync.WaitGroup

	st := &restoreState{
		client:     client,
		target:     target,
		fsCap:      fsCap,
		noAttr:     noAttr,
		hl:         newHardlinkIndex(),
		jobs:       jobs,
		wg:         &wg,
		onProgress: opts.OnProgress,
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	log.Info("restore: starting content restore",

		"workers", numWorkers, "dest", destDir, "sources", len(sources))

	var workersWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workersWg.Go(func() {
			for job := range jobs {
				func() {
					defer wg.Done()
					if err := st.runJobRecovered(workerCtx, client, job); err != nil {
						if serr := client.SendError(workerCtx, err); serr != nil {
							log.Error(err, "", "sendErr", serr.Error(), "restore", "error-report-failed")

						}
						return
					}
					st.recordProgress(job)
				}()
			}
		})
	}

	var sourcesWg sync.WaitGroup
	for _, source := range sources {
		if workerCtx.Err() != nil {
			break
		}

		sourcesWg.Add(1)
		go func(src string) {
			defer sourcesWg.Done()
			if workerCtx.Err() != nil {
				return
			}

			sourceAttr, err := client.LookupByPath(workerCtx, src)
			if err != nil {
				if serr := client.SendError(workerCtx, err); serr != nil {
					log.Error(err, "", "sendErr", serr.Error(), "restore", "error-report-failed")

				}
				return
			}
			name := sourceAttr.Name()
			// Native Windows archives name their root directory with a
			// backslash. Treat only that directory marker as the destination
			// itself; child names still pass the strict component checks.
			if sourceAttr.FileType == pxar.FileTypeDirectory && (name == "/" || (filepath.Separator == '\\' && name == "\\")) {
				name = ""
			}
			if name != "" && name != "/" && name != "." {
				if err := validRestoreName(name); err != nil {
					_ = client.SendError(workerCtx, err)
					return
				}
			}
			dest := filepath.Join(destDir, name)
			srcPath := strings.TrimPrefix(filepath.ToSlash(name), "/")

			wg.Add(1)
			select {
			case jobs <- restoreJob{dest: dest, srcPath: srcPath, info: sourceAttr}:
			case <-workerCtx.Done():
				wg.Done()
			}
		}(source)
	}

	sourcesWg.Wait()
	wg.Wait()
	close(jobs)
	workersWg.Wait()
	log.Info("restore: all workers drained, resolving deferred hardlinks")

	func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic resolving deferred hardlinks: %v\n%s", r, debug.Stack())
				log.Error(err, "", "restore", "panic")
				if err := client.SendError(workerCtx, err); err != nil {
					log.Error(err, "")
				}
			}
		}()
		if err := resolveDeferredHardlinks(workerCtx, st); err != nil {
			if serr := client.SendError(workerCtx, err); serr != nil {
				log.Error(err, "", "sendErr", serr.Error(), "restore", "error-report-failed")

			}
		}
	}()

	if err := applyDeferredDirMeta(workerCtx, st); err != nil {
		if serr := client.SendError(workerCtx, err); serr != nil {
			log.Error(err, "", "sendErr", serr.Error(), "restore", "error-report-failed")
		}
	}

	log.Info("restore: content restore complete, returning to caller")

	return ctx.Err()
}

// restored during the concurrent pass. Runs after the worker pool drains.
func resolveDeferredHardlinks(ctx context.Context, st *restoreState) error {
	var errs []error
	for _, job := range st.hl.drainDeferred() {
		if target, ok := st.hl.tryResolveNow(job); ok {
			if err := st.linkFile(target, job.dest); err != nil {
				if ferr := restoreFileContent(ctx, st, job); ferr != nil {
					errs = append(errs, fmt.Errorf("hardlink %q: %w", job.dest, ferr))
				}
			}
		} else {
			if err := restoreFileContent(ctx, st, job); err != nil {
				errs = append(errs, fmt.Errorf("hardlink %q (target %q not in scope): %w", job.dest, job.info.LinkTarget, err))
			}
		}
		st.hl.register(job.srcPath, job.dest)
	}
	return errors.Join(errs...)
}

func processJob(ctx context.Context, st *restoreState, job restoreJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	var err error
	switch job.info.FileType {
	case pxar.FileTypeDirectory:
		err = restoreDir(ctx, st, job)
	case pxar.FileTypeFifo, pxar.FileTypeSocket:
		err = restoreSpecial(ctx, st, job)
	case pxar.FileTypeSymlink:
		err = restoreSymlink(ctx, st, job)
	case pxar.FileTypeFile:
		err = restoreFile(ctx, st, job)
	case pxar.FileTypeHardlink:
		if target, ok := st.hl.tryResolveNow(job); ok {
			if lerr := st.linkFile(target, job.dest); lerr != nil {
				if cerr := restoreFileContent(ctx, st, job); cerr != nil {
					return fmt.Errorf("hardlink %q: link failed (%v) and fallback failed: %w", job.dest, lerr, cerr)
				}
			}
		} else {
			st.hl.deferLink(job)
			return nil
		}
	default:
		return nil
	}
	if err != nil {
		return err
	}
	st.hl.register(job.srcPath, job.dest)
	return nil
}

// A matching size/mtime cannot prove content equality. Both restore modes
// write the archived payload; content-only controls metadata application only.
func restoreFile(ctx context.Context, st *restoreState, job restoreJob) error {
	return restoreFileContent(ctx, st, job)
}

// restoreFileContent writes to a hidden temp, atomically swaps it into place,
// destination, never the temp: on Windows, a restrictive DACL or READONLY
// attribute on the temp blocks rename/cleanup, leaking .pxar-restore-* files.
func restoreFileContent(ctx context.Context, st *restoreState, job restoreJob) error {
	e := job.info
	if e.FileType == pxar.FileTypeHardlink && (e.RawSize == 0 || e.ContentRange == nil) {
		return fmt.Errorf("hardlink %q (target %q) has no restorable content", job.dest, e.LinkTarget)
	}

	tmpPath, err := streamToTemp(ctx, st, job)
	if err != nil {
		return err
	}

	target, closeTarget, err := st.destinationFS(job.dest)
	if err != nil {
		return err
	}
	defer closeTarget()
	if err := target.swap(tmpPath, job.dest); err != nil {
		return err
	}

	if st.noAttr {
		return applyTempMode(st, job.dest, e.RawMode)
	}
	return restoreMetadata(ctx, st, job.dest, e, false)
}

// (so the final rename is same-filesystem atomic). On error the temp is
func streamToTemp(ctx context.Context, st *restoreState, job restoreJob) (tmpPath string, err error) {
	e := job.info
	dir := filepath.Dir(job.dest)

	target, closeTarget, err := st.destinationFS(job.dest)
	if err != nil {
		return "", err
	}
	defer closeTarget()
	f, err := target.temporary(dir)
	if err != nil {
		return "", fmt.Errorf("create temp for %q: %w", job.dest, err)
	}
	tmpPath = f.Name()

	defer func() {
		if err != nil {
			if cerr := f.Close(); cerr != nil {
				log.Error(cerr, "")
			}
			if rmErr := target.remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
				err = fmt.Errorf("%w (temp cleanup of %q failed: %v)", err, tmpPath, rmErr)
			}
			tmpPath = ""
		}
	}()

	if e.RawSize > 0 && len(e.ContentRange) != 2 {
		return tmpPath, fmt.Errorf("missing file content range")
	}
	if e.RawSize > 0 {
		rc, rerr := st.client.ReadFileContentReader(ctx, e.ContentRange[0], e.ContentRange[1], e.RawSize)
		if rerr != nil {
			return tmpPath, fmt.Errorf("open content reader %q: %w", job.dest, rerr)
		}
		const bufSize = 256 * 1024
		buf := make([]byte, bufSize)
		n, werr := io.CopyBuffer(f, rc, buf)
		if werr == nil && uint64(n) != e.RawSize {
			werr = fmt.Errorf("restored content size mismatch: got %d, want %d", n, e.RawSize)
		}
		if cerr := rc.Close(); cerr != nil && werr == nil {
			werr = cerr
		}
		if werr != nil {
			return tmpPath, fmt.Errorf("copy data %q: %w", job.dest, werr)
		}
	}

	if err = f.Sync(); err != nil {
		return tmpPath, fmt.Errorf("sync temp %q: %w", job.dest, err)
	}
	if err = f.Close(); err != nil {
		return tmpPath, fmt.Errorf("close temp %q: %w", job.dest, err)
	}
	return tmpPath, nil
}

// Standalone wrapper retained for callers testing the atomic swap contract.
func atomicSwap(tmpPath, dest string) error {
	target, err := openRestoreFS(filepath.Dir(dest))
	if err != nil {
		return err
	}
	defer target.root.Close()
	return target.swap(tmpPath, dest)
}
func (st *restoreState) linkFile(old, new string) error {
	target, closeTarget, err := st.destinationFS(new)
	if err != nil {
		return err
	}
	defer closeTarget()
	return target.link(old, new)
}

func restoreSymlink(ctx context.Context, st *restoreState, job restoreJob) error {
	fs, closeTarget, err := st.destinationFS(job.dest)
	if err != nil {
		return err
	}
	defer closeTarget()
	e := job.info
	target, err := st.client.ReadLink(ctx, e.EntryRangeStart, e.EntryRangeEnd)
	if err != nil {
		return fmt.Errorf("readlink data %q: %w", job.dest, err)
	}

	if existingTarget, err := fs.readlink(job.dest); err == nil {
		if existingTarget == string(target) {
			if !st.noAttr {
				return applyMetaSymlink(ctx, st, job.dest, e)
			}
			return nil
		}
		if err := fs.remove(job.dest); err != nil && !os.IsNotExist(err) {
			log.Error(err, "")
		}
	}

	if err := fs.symlink(string(target), job.dest); err != nil {
		return fmt.Errorf("symlink %q: %w", job.dest, err)
	}

	if st.noAttr {
		return nil
	}

	return applyMetaSymlink(ctx, st, job.dest, e)
}
