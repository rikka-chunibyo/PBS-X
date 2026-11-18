package fswire

import (
	"fmt"
	"time"
)

type (
	OpenFileReq struct {
		Path string `cbor:"path"`
		Flag int    `cbor:"flag"`
		Perm int    `cbor:"perm"`
	}

	StatReq struct {
		Path    string `cbor:"path"`
		AclOnly bool   `cbor:"acl_only,omitempty"`
	}

	XAttrReq struct {
		Path    string `cbor:"path"`
		Attr    string `cbor:"attr"`
		AclOnly bool   `cbor:"acl_only,omitempty"`
	}

	FileHandleID uint64

	ReadDirReq struct {
		HandleID FileHandleID `cbor:"handle_id"`
	}

	ReadReq struct {
		HandleID FileHandleID `cbor:"handle_id"`
		Length   int          `cbor:"length"`
	}

	ReadAtReq struct {
		HandleID FileHandleID `cbor:"handle_id"`
		Offset   int64        `cbor:"offset"`
		Length   int          `cbor:"length"`
	}

	CloseReq struct {
		HandleID FileHandleID `cbor:"handle_id"`
	}

	BackupReq struct {
		BackupID            string   `cbor:"job_id"`
		HostID              string   `cbor:"host_id,omitempty"`
		Store               string   `cbor:"store,omitempty"`
		Namespace           string   `cbor:"namespace,omitempty"`
		Sources             []string `cbor:"sources,omitempty"`
		SnapshotMode        string   `cbor:"snapshot_mode,omitempty"`
		ChangeDetectionMode string   `cbor:"change_detection_mode,omitempty"`

		// Legacy PBS Plus/PBS-X bridge fields. Kept during migration so older
		// control-plane callers fail gracefully rather than breaking decoding.
		Drive      string `cbor:"drive,omitempty"`
		SourceMode string `cbor:"source_mode,omitempty"`
		ReadMode   string `cbor:"read_mode,omitempty"`
		Extras     string `cbor:"extras,omitempty"`
	}

	RestoreReq struct {
		RestoreID      string `cbor:"job_id"`
		SrcPath        string `cbor:"src_path"`
		DestPath       string `cbor:"dest_path"`
		Mode           int    `cbor:"mode"`
		IdempotencyKey string `cbor:"idempotency_key,omitempty"`
		Extras         string `cbor:"extras,omitempty"`
	}

	RestoreCloseReq struct {
		RestoreID string `cbor:"job_id"`
	}

	FileTreeReq struct {
		HostPath string `cbor:"host_path"`
		SubPath  string `cbor:"subpath"`
		Extras   string `cbor:"extras,omitempty"`
	}

	FileTreeEntry struct {
		Filepath string `json:"filepath"`
		Leaf     bool   `json:"leaf"`
		Text     string `json:"text"`
		Type     string `json:"type"`
		Mtime    int64  `json:"mtime,omitempty"`
		Size     int64  `json:"size,omitempty"`
	}

	FileTreeResp struct {
		Data []FileTreeEntry `json:"data"`
	}

	LseekReq struct {
		HandleID FileHandleID `cbor:"handle_id"`
		Offset   int64        `cbor:"offset"`
		Whence   int          `cbor:"whence"`
	}

	TargetStatusReq struct {
		Drive   string `cbor:"drive"`
		Subpath string `cbor:"subpath,omitempty"`
	}

	TargetStatusBatchReq struct {
		Drives []TargetStatusReq `cbor:"drives"`
	}

	TargetStatusBatchResp struct {
		Version string                       `cbor:"version"`
		Drives  map[string]TargetDriveStatus `cbor:"drives"`
	}

	TargetDriveStatus struct {
		Reachable *bool  `cbor:"reachable,omitempty"`
		Message   string `cbor:"message,omitempty"`
	}

	LseekResp struct {
		NewOffset int64 `cbor:"new_offset"`
	}

	WinACL struct {
		SID        string `cbor:"sid"`
		AccessMask uint32 `cbor:"access_mask"`
		Type       uint8  `cbor:"type"`
		Flags      uint8  `cbor:"flags"`
	}

	PosixACL struct {
		Tag       string `cbor:"tag"`
		ID        int32  `cbor:"id"`
		Perms     uint8  `cbor:"perms"`
		IsDefault bool   `cbor:"is_default"`
	}

	AgentFileInfo struct {
		Name    string `cbor:"name"`
		Size    int64  `cbor:"size"`
		Mode    uint32 `cbor:"mode"`
		ModTime int64  `cbor:"mod_time"`
		// Native precision is kept separate from the legacy wire timestamp units.
		NativeModTime  *time.Time      `cbor:"-" json:"-"`
		IsDir          bool            `cbor:"is_dir"`
		Blocks         uint64          `cbor:"blocks,omitempty"`
		CreationTime   int64           `cbor:"creation_time,omitempty"`
		LastAccessTime int64           `cbor:"last_access_time,omitempty"`
		LastWriteTime  int64           `cbor:"last_write_time,omitempty"`
		FileAttributes map[string]bool `cbor:"file_attributes,omitempty"`
		Owner          string          `cbor:"owner,omitempty"`
		Group          string          `cbor:"group,omitempty"`
		WinACLs        []WinACL        `cbor:"win_acls,omitempty"`
		DACLProtected  *bool           `cbor:"dacl_protected,omitempty"`
		PosixACLs      []PosixACL      `cbor:"posix_acls,omitempty"`
	}

	StatFS struct {
		Bsize   uint64 `cbor:"bsize"`
		Blocks  uint64 `cbor:"blocks"`
		Bfree   uint64 `cbor:"bfree"`
		Bavail  uint64 `cbor:"bavail"`
		Files   uint64 `cbor:"files"`
		Ffree   uint64 `cbor:"ffree"`
		NameLen uint64 `cbor:"name_len"`
	}

	ReadDirEntries []AgentFileInfo
)

type DriveInfo struct {
	Letter     string `json:"letter"`
	Type       string `json:"type"`
	VolumeName string `json:"volume_name"`
	FileSystem string `json:"filesystem"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	Total      string `json:"total"`
	Used       string `json:"used"`
	Free       string `json:"free"`
}

func HumanizeBytes(bytes uint64) string {
	const unit = 1000
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := unit, 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	var unitSymbol string
	switch exp {
	case 0:
		unitSymbol = "KB"
	case 1:
		unitSymbol = "MB"
	case 2:
		unitSymbol = "GB"
	case 3:
		unitSymbol = "TB"
	case 4:
		unitSymbol = "PB"
	default:
		unitSymbol = "??"
	}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), unitSymbol)
}
