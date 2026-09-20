package snapshots

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

// Snapshot is a point-in-time filesystem view suitable for direct PXAR walking.
// Path is always the browsable filesystem root for the selected source. BackendPath
// is provider-private state used to tear the snapshot down (for example a ZFS
// dataset@snapshot name or an LVM snapshot block device).
type Snapshot struct {
	Path        string    `json:"path"`
	BackendPath string    `json:"backend_path,omitempty"`
	TimeStarted time.Time `json:"time_started"`
	SourcePath  string    `json:"source_path"`
	Direct      bool      `json:"direct"`
	// Excludes are paths relative to Path that the PXAR walker must never enter.
	// Snapshot providers use this for their own management namespaces; the
	// native runner appends user-configured exclusions.
	Excludes []string        `json:"-"`
	Handler  SnapshotHandler `json:"-"`
}

// IsZero reports whether the snapshot contains no provider or direct-mode state.
func (s Snapshot) IsZero() bool {
	return s.Path == "" &&
		s.BackendPath == "" &&
		s.TimeStarted.IsZero() &&
		s.SourcePath == "" &&
		!s.Direct &&
		len(s.Excludes) == 0 &&
		s.Handler == nil
}

type SnapshotHandler interface {
	CreateSnapshot(jobID string, sourcePath string) (Snapshot, error)
	DeleteSnapshot(snapshot Snapshot) error
	IsSupported(sourcePath string) bool
}

func safeSnapshotComponent(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "backup"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

var (
	ErrSnapshotTimeout  = errors.New("timeout waiting for in-progress snapshot")
	ErrSnapshotCreation = errors.New("failed to create snapshot")
	ErrInvalidSnapshot  = errors.New("invalid snapshot")
)
