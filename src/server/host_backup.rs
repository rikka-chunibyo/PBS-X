//! PBS-X host backup orchestration primitives.
//!
//! Generation 1 uses an agent snapshot exposed to the PBS-X server and then runs the
//! normal `proxmox-backup-client`. The most important invariant here is that the PBS
//! backup group ID is the Host ID, never the backup job ID.

use pbsx_api_types::{HostBackupJobConfig, HostChangeDetectionMode};

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HostArchiveSource {
    /// PXAR archive name without the `.pxar` suffix, for example `c` or `root`.
    pub archive_name: String,
    /// Local path exposed by the host data-plane mount.
    pub path: String,
}

fn change_detection_arg(mode: HostChangeDetectionMode) -> &'static str {
    match mode {
        HostChangeDetectionMode::Metadata => "--change-detection-mode=metadata",
        HostChangeDetectionMode::Data => "--change-detection-mode=data",
        HostChangeDetectionMode::Legacy => "--change-detection-mode=legacy",
    }
}

/// Build `proxmox-backup-client backup` arguments for a native PBS-X host job.
///
/// Authentication/environment setup and process execution deliberately live outside this helper.
/// This makes the host-group identity easy to test and hard to accidentally change while the
/// aRPC/FUSE data plane is being ported.
pub fn build_backup_client_args(
    job: &HostBackupJobConfig,
    repository: &str,
    archives: &[HostArchiveSource],
) -> Vec<String> {
    let mut args = vec!["backup".to_string()];

    for archive in archives {
        args.push(format!("{}.pxar:{}", archive.archive_name, archive.path));
    }

    args.extend([
        "--repository".to_string(),
        repository.to_string(),
        "--backup-type".to_string(),
        "host".to_string(),
        "--backup-id".to_string(),
        job.backup_id().to_string(),
        change_detection_arg(job.change_detection_mode).to_string(),
    ]);

    if let Some(ns) = &job.ns {
        if !ns.is_root() {
            args.push("--ns".to_string());
            args.push(ns.to_string());
        }
    }

    args
}

#[cfg(test)]
mod tests {
    use super::*;
    use pbsx_api_types::{HostBackupJobConfig, HostSnapshotMode};

    fn test_job() -> HostBackupJobConfig {
        HostBackupJobConfig {
            id: "nightly-gaming-pc".to_string(),
            host: "gaming-pc".to_string(),
            store: "main".to_string(),
            ns: None,
            sources: vec!["C:\\".to_string()],
            schedule: Some("daily".to_string()),
            snapshot_mode: HostSnapshotMode::Auto,
            change_detection_mode: HostChangeDetectionMode::Metadata,
            enable: true,
            comment: None,
        }
    }

    #[test]
    fn backup_group_is_host_not_job() {
        let job = test_job();
        let args = build_backup_client_args(
            &job,
            "pbsx@pbs!hostd@localhost:main",
            &[HostArchiveSource {
                archive_name: "c".to_string(),
                path: "/run/pbsx/host-mount/nightly-gaming-pc/c".to_string(),
            }],
        );

        let backup_id = args
            .windows(2)
            .find(|pair| pair[0] == "--backup-id")
            .map(|pair| pair[1].as_str());

        assert_eq!(backup_id, Some("gaming-pc"));
        assert!(!args.iter().any(|arg| arg == "nightly-gaming-pc"));
    }
}
