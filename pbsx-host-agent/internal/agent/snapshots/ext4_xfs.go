package snapshots

import "fmt"

type EXT4XFSHandler struct{}

func (e *EXT4XFSHandler) CreateSnapshot(jobID string, sourcePath string) (Snapshot, error) {
	fsType, err := detectFilesystem(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	if fsType != "ext4" && fsType != "xfs" {
		return Snapshot{}, fmt.Errorf("source path %q is %s, not ext4/xfs", sourcePath, fsType)
	}
	lvm := &LVMSnapshotHandler{}
	if !lvm.IsSupported(sourcePath) {
		return Snapshot{}, fmt.Errorf("%s source %q is not backed by an LVM logical volume", fsType, sourcePath)
	}
	return lvm.CreateSnapshot(jobID, sourcePath)
}

func (e *EXT4XFSHandler) DeleteSnapshot(snapshot Snapshot) error {
	return (&LVMSnapshotHandler{}).DeleteSnapshot(snapshot)
}

func (e *EXT4XFSHandler) IsSupported(sourcePath string) bool {
	fsType, err := detectFilesystem(sourcePath)
	if err != nil || (fsType != "ext4" && fsType != "xfs") {
		return false
	}
	return (&LVMSnapshotHandler{}).IsSupported(sourcePath)
}
