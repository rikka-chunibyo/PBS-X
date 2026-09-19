package snapshots

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type mountInfo struct {
	Source string
	Target string
	FSType string
}

func findMount(sourcePath string) (mountInfo, error) {
	if runtime.GOOS != "linux" {
		return mountInfo{}, fmt.Errorf("mount lookup is only available on Linux")
	}
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return mountInfo{}, err
	}

	// JSON avoids corrupting mount paths containing spaces or findmnt escape sequences.
	out, err := exec.Command("findmnt", "--json", "-T", abs, "-o", "SOURCE,TARGET,FSTYPE").Output()
	if err != nil {
		return mountInfo{}, fmt.Errorf("find mount for %q: %w", sourcePath, err)
	}
	var payload struct {
		Filesystems []struct {
			Source string `json:"source"`
			Target string `json:"target"`
			FSType string `json:"fstype"`
		} `json:"filesystems"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return mountInfo{}, fmt.Errorf("parse findmnt output for %q: %w", sourcePath, err)
	}
	if len(payload.Filesystems) == 0 {
		return mountInfo{}, fmt.Errorf("findmnt returned no mount for %q", sourcePath)
	}
	fs := payload.Filesystems[0]
	if fs.Source == "" || fs.Target == "" || fs.FSType == "" {
		return mountInfo{}, fmt.Errorf("findmnt returned incomplete mount data for %q", sourcePath)
	}
	return mountInfo{Source: fs.Source, Target: fs.Target, FSType: strings.ToLower(fs.FSType)}, nil
}

func detectFilesystem(sourcePath string) (string, error) {
	switch runtime.GOOS {
	case "linux":
		info, err := findMount(sourcePath)
		if err != nil {
			return "", err
		}
		return info.FSType, nil
	case "darwin":
		cmd := exec.Command("diskutil", "info", sourcePath)
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to detect filesystem type: %w", err)
		}
		for line := range strings.SplitSeq(string(output), "\n") {
			if strings.Contains(line, "File System Personality") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					return strings.ToLower(strings.TrimSpace(parts[1])), nil
				}
			}
		}
		return "", fmt.Errorf("could not determine filesystem type from diskutil output")
	case "windows":
		return "ntfs", nil
	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}
