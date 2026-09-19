//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func collectInventory() inventory {
	return inventory{
		Disks:       linuxDisks(),
		Filesystems: linuxFilesystems(),
	}
}

func linuxDisks() []diskInfo {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}

	disks := make([]diskInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}

		base := filepath.Join("/sys/block", name)
		sectors, _ := readUint(filepath.Join(base, "size"))
		rotationalValue, rotationalErr := readUint(filepath.Join(base, "queue", "rotational"))
		var rotational *bool
		if rotationalErr == nil {
			value := rotationalValue != 0
			rotational = &value
		}

		disks = append(disks, diskInfo{
			Name:       "/dev/" + name,
			Size:       sectors * 512,
			Model:      readTrimmed(filepath.Join(base, "device", "model")),
			Serial:     readTrimmed(filepath.Join(base, "device", "serial")),
			Rotational: rotational,
		})
	}
	return disks
}

func linuxFilesystems() []filesystemInfo {
	file, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer file.Close()

	seen := make(map[string]bool)
	var filesystems []filesystemInfo
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		source := unescapeMountField(fields[0])
		mountpoint := unescapeMountField(fields[1])
		fsType := fields[2]
		if seen[mountpoint] {
			continue
		}
		seen[mountpoint] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountpoint, &stat); err != nil {
			continue
		}
		blockSize := uint64(stat.Bsize)
		filesystems = append(filesystems, filesystemInfo{
			Mountpoint: mountpoint,
			FileSystem: fsType,
			Source:     source,
			Total:      stat.Blocks * blockSize,
			Available:  stat.Bavail * blockSize,
		})
	}
	return filesystems
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readUint(path string) (uint64, error) {
	value := readTrimmed(path)
	return strconv.ParseUint(value, 10, 64)
}

func unescapeMountField(value string) string {
	replacer := strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	)
	return replacer.Replace(value)
}
