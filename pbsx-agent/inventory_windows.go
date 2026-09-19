//go:build windows

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

type windowsDisk struct {
	DeviceID string `json:"DeviceID"`
	Size     uint64 `json:"Size"`
	Model    string `json:"Model"`
	Serial   string `json:"SerialNumber"`
}

type windowsFilesystem struct {
	DeviceID   string `json:"DeviceID"`
	FileSystem string `json:"FileSystem"`
	Size       uint64 `json:"Size"`
	FreeSpace  uint64 `json:"FreeSpace"`
}

func collectInventory() inventory {
	return inventory{
		Disks:       windowsDisks(),
		Filesystems: windowsFilesystems(),
	}
}

func powershellJSON(script string, target any) error {
	output, err := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		script,
	).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(output, target)
}

func windowsDisks() []diskInfo {
	var source []windowsDisk
	script := `$items = @(Get-CimInstance Win32_DiskDrive | Select-Object DeviceID,Size,Model,SerialNumber); ConvertTo-Json -Compress -InputObject $items`
	if err := powershellJSON(script, &source); err != nil {
		return nil
	}

	result := make([]diskInfo, 0, len(source))
	for _, item := range source {
		result = append(result, diskInfo{
			Name:   strings.TrimSpace(item.DeviceID),
			Size:   item.Size,
			Model:  strings.TrimSpace(item.Model),
			Serial: strings.TrimSpace(item.Serial),
		})
	}
	return result
}

func windowsFilesystems() []filesystemInfo {
	var source []windowsFilesystem
	script := `$items = @(Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | Select-Object DeviceID,FileSystem,Size,FreeSpace); ConvertTo-Json -Compress -InputObject $items`
	if err := powershellJSON(script, &source); err != nil {
		return nil
	}

	result := make([]filesystemInfo, 0, len(source))
	for _, item := range source {
		mountpoint := strings.TrimSpace(item.DeviceID)
		if mountpoint != "" && !strings.HasSuffix(mountpoint, `\`) {
			mountpoint += `\`
		}
		result = append(result, filesystemInfo{
			Mountpoint: mountpoint,
			FileSystem: strings.TrimSpace(item.FileSystem),
			Source:     strings.TrimSpace(item.DeviceID),
			Total:      item.Size,
			Available:  item.FreeSpace,
		})
	}
	return result
}
