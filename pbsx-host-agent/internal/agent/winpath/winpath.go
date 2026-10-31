// Package winpath implements the deterministic subset of Windows path
// semantics needed by the backup pipeline without depending on GOOS=windows.
// This lets Linux tests exercise the same drive/UNC routing used by the agent.
package winpath

import (
	"fmt"
	"path"
	"strings"
)

// Path is an absolute Windows path split into a volume and clean components.
// Volume is a drive such as "C:" or a UNC share such as `\\server\share`.
type Path struct {
	Volume string
	Parts  []string
}

// ParseAbsolute parses an absolute drive or UNC path using Windows separators.
func ParseAbsolute(value string) (Path, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Path{}, fmt.Errorf("empty Windows path")
	}
	normalized := strings.ReplaceAll(raw, `\`, "/")

	var volume, tail string
	switch {
	case len(normalized) >= 3 && isDriveLetter(normalized[0]) && normalized[1] == ':' && normalized[2] == '/':
		volume = strings.ToUpper(normalized[:2])
		tail = normalized[3:]
	case strings.HasPrefix(normalized, "//"):
		components := strings.Split(strings.TrimPrefix(normalized, "//"), "/")
		if len(components) < 2 || components[0] == "" || components[1] == "" {
			return Path{}, fmt.Errorf("invalid UNC path %q", value)
		}
		volume = `\\` + components[0] + `\` + components[1]
		tail = strings.Join(components[2:], "/")
	default:
		return Path{}, fmt.Errorf("Windows path %q is not absolute", value)
	}

	parts, err := cleanParts(tail, false)
	if err != nil {
		return Path{}, fmt.Errorf("clean Windows path %q: %w", value, err)
	}
	return Path{Volume: volume, Parts: parts}, nil
}

// CleanRelative cleans a source-relative Windows path and rejects traversal.
func CleanRelative(value string) (string, error) {
	parts, err := cleanParts(strings.ReplaceAll(strings.TrimSpace(value), `\`, "/"), true)
	if err != nil {
		return "", err
	}
	return strings.Join(parts, "/"), nil
}

// RelativeTo returns target relative to base. Inside is false for another
// volume or a path outside base. Comparisons use Windows case folding.
func RelativeTo(base, target Path) (relative string, inside bool) {
	if !strings.EqualFold(base.Volume, target.Volume) || len(target.Parts) < len(base.Parts) {
		return "", false
	}
	for i := range base.Parts {
		if !strings.EqualFold(base.Parts[i], target.Parts[i]) {
			return "", false
		}
	}
	return strings.Join(target.Parts[len(base.Parts):], "/"), true
}

// WithRootRelative resolves a path beginning with slash/backslash on base's volume.
func WithRootRelative(base Path, value string) (Path, error) {
	value = strings.TrimLeft(strings.TrimSpace(value), `\/`)
	prefix := base.Volume + `\`
	return ParseAbsolute(prefix + value)
}

// RelativeWindows returns the path below a volume using Windows separators.
func (p Path) RelativeWindows() string { return strings.Join(p.Parts, `\`) }

// Alias returns the stable top-level archive name for this Windows source.
func (p Path) Alias() string {
	base := strings.TrimSuffix(p.Volume, ":")
	base = strings.TrimPrefix(base, `\\`)
	base = strings.NewReplacer(`\`, "-", "/", "-").Replace(base)
	if len(p.Parts) != 0 {
		base += "-" + strings.Join(p.Parts, "-")
	}
	return sanitizeAlias(base)
}

func cleanParts(value string, rejectParent bool) ([]string, error) {
	if value == "" || value == "." {
		return nil, nil
	}
	if rejectParent {
		for _, component := range strings.Split(value, "/") {
			if component == ".." {
				return nil, fmt.Errorf("parent traversal is not allowed")
			}
		}
	}
	cleaned := strings.TrimPrefix(path.Clean("/"+value), "/")
	if cleaned == "." || cleaned == "" {
		return nil, nil
	}
	return strings.Split(cleaned, "/"), nil
}

func sanitizeAlias(value string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	alias := strings.Trim(out.String(), "-.")
	if alias == "" {
		alias = "source"
	}
	if len(alias) > 64 {
		alias = alias[:64]
	}
	return alias
}

func isDriveLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
