package main

type diskInfo struct {
	Name       string `json:"name"`
	Size       uint64 `json:"size,omitempty"`
	Model      string `json:"model,omitempty"`
	Serial     string `json:"serial,omitempty"`
	Rotational *bool  `json:"rotational,omitempty"`
}

type filesystemInfo struct {
	Mountpoint string `json:"mountpoint"`
	FileSystem string `json:"file-system"`
	Source     string `json:"source,omitempty"`
	Total      uint64 `json:"total,omitempty"`
	Available  uint64 `json:"available,omitempty"`
}

type inventory struct {
	Disks       []diskInfo
	Filesystems []filesystemInfo
}
