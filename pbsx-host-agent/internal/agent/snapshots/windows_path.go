package snapshots

import "github.com/pbs-x/pbsx-host-agent/internal/agent/winpath"

type windowsSnapshotSource struct {
	Volume   string
	Relative string
}

// planWindowsSnapshotSource is the deterministic half of VSS source mapping.
// It is intentionally portable so Linux tests can validate the paths which
// the Windows-only VSS calls will receive.
func planWindowsSnapshotSource(absoluteSource string) (windowsSnapshotSource, error) {
	parsed, err := winpath.ParseAbsolute(absoluteSource)
	if err != nil {
		return windowsSnapshotSource{}, err
	}
	return windowsSnapshotSource{
		Volume:   parsed.Volume,
		Relative: parsed.RelativeWindows(),
	}, nil
}
