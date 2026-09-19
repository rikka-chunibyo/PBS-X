package agentfs

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	pxarlib "github.com/pbs-plus/pxar"
	"github.com/pbs-plus/pxar/backupproxy"
	"github.com/pbs-plus/pxar/format"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/snapshots"
)

// VirtualSource is one configured host-backup source exposed below the virtual
// PXAR root. Alias must be unique within the backup run.
type VirtualSource struct {
	Alias    string
	Snapshot snapshots.Snapshot
	Accessor *NativeAccessor
}

// VirtualAccessor presents one or more point-in-time source snapshots as a
// single synthetic filesystem. This keeps one PBS host snapshot per job run
// while allowing Windows C:/D: or multiple Linux mountpoints to be protected.
type VirtualAccessor struct {
	sources []VirtualSource
	byAlias map[string]*VirtualSource
}

func NewVirtualAccessor(jobID, readMode string, snaps []snapshots.Snapshot) (*VirtualAccessor, error) {
	if len(snaps) == 0 {
		return nil, fmt.Errorf("native backup requires at least one source snapshot")
	}
	v := &VirtualAccessor{byAlias: make(map[string]*VirtualSource, len(snaps))}
	used := make(map[string]int)
	for _, snap := range snaps {
		base := sourceAlias(snap.SourcePath)
		used[base]++
		alias := base
		if used[base] > 1 {
			alias = fmt.Sprintf("%s-%d", base, used[base])
		}
		source := VirtualSource{
			Alias:    alias,
			Snapshot: snap,
			Accessor: NewNativeAccessor(jobID, readMode, snap),
		}
		v.sources = append(v.sources, source)
	}
	for i := range v.sources {
		s := &v.sources[i]
		v.byAlias[s.Alias] = s
	}
	sort.Slice(v.sources, func(i, j int) bool { return v.sources[i].Alias < v.sources[j].Alias })
	return v, nil
}

func (v *VirtualAccessor) Close() {
	for i := range v.sources {
		v.sources[i].Accessor.Close()
	}
}

func (v *VirtualAccessor) Stat(path string) (format.Stat, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return format.Stat{}, err
	}
	if root {
		return format.Stat{Mode: format.ModeIFDIR | 0o755}, nil
	}
	return source.Accessor.Stat(sub)
}

func (v *VirtualAccessor) ReadDir(path string) ([]backupproxy.DirEntry, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return nil, err
	}
	if root {
		out := make([]backupproxy.DirEntry, 0, len(v.sources))
		for i := range v.sources {
			s := &v.sources[i]
			st, err := s.Accessor.Stat(".")
			if err != nil {
				return nil, fmt.Errorf("stat source %q: %w", s.Snapshot.SourcePath, err)
			}
			// Every top-level source is represented as a directory even if the
			// selected path happens to resolve through platform-specific syntax.
			st.Mode = (st.Mode &^ format.ModeIFMT) | format.ModeIFDIR
			out = append(out, backupproxy.DirEntry{Name: s.Alias, Stat: st})
		}
		return out, nil
	}
	return source.Accessor.ReadDir(sub)
}

func (v *VirtualAccessor) OpenFile(path string) (io.ReadCloser, uint64, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return nil, 0, err
	}
	if root {
		return nil, 0, fmt.Errorf("virtual root is a directory")
	}
	return source.Accessor.OpenFile(sub)
}

func (v *VirtualAccessor) ReadLink(path string) (string, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return "", err
	}
	if root {
		return "", fmt.Errorf("virtual root is not a symlink")
	}
	return source.Accessor.ReadLink(sub)
}

func (v *VirtualAccessor) GetXAttrs(path string) ([]format.XAttr, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return nil, err
	}
	if root {
		return nil, nil
	}
	return source.Accessor.GetXAttrs(sub)
}

func (v *VirtualAccessor) GetACL(path string) (pxarlib.ACL, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return pxarlib.ACL{}, err
	}
	if root {
		return pxarlib.ACL{}, nil
	}
	return source.Accessor.GetACL(sub)
}

func (v *VirtualAccessor) GetFCaps(path string) ([]byte, error) {
	source, sub, root, err := v.resolve(path)
	if err != nil {
		return nil, err
	}
	if root {
		return nil, nil
	}
	return source.Accessor.GetFCaps(sub)
}

func (v *VirtualAccessor) resolve(path string) (*VirtualSource, string, bool, error) {
	clean := strings.TrimSpace(path)
	clean = strings.ReplaceAll(clean, `\\`, "/")
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimPrefix(clean, "./")
	clean = strings.Trim(clean, "/")
	if clean == "" || clean == "." {
		return nil, ".", true, nil
	}
	parts := strings.SplitN(clean, "/", 2)
	source := v.byAlias[parts[0]]
	if source == nil {
		return nil, "", false, fmt.Errorf("unknown virtual backup source %q", parts[0])
	}
	sub := "."
	if len(parts) == 2 && parts[1] != "" {
		sub = filepath.FromSlash(parts[1])
	}
	return source, sub, false, nil
}

func sourceAlias(source string) string {
	clean := strings.TrimSpace(source)
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(clean)
		rest := strings.Trim(strings.TrimPrefix(clean, volume), `\\/`)
		base := strings.TrimSuffix(volume, ":")
		if base == "" {
			base = "volume"
		}
		if rest != "" {
			base += "-" + strings.ReplaceAll(strings.ReplaceAll(rest, `\\`, "-"), "/", "-")
		}
		return sanitizeAlias(base)
	}
	if clean == "/" || clean == "" {
		return "root"
	}
	clean = strings.Trim(clean, "/")
	if clean == "" {
		return "root"
	}
	return sanitizeAlias(strings.ReplaceAll(clean, "/", "-"))
}

func sanitizeAlias(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		out = "source"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
