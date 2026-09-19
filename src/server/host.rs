//! Runtime state for PBS-X protected hosts.
//!
//! Static host definitions live in `/etc/proxmox-backup/host.cfg`. Agent heartbeats are written
//! separately under the PBS state directory so a heartbeat does not constantly mutate the admin
//! configuration or its digest.

use std::path::{Path, PathBuf};

use anyhow::{format_err, Error};
use nix::sys::stat::Mode;

use proxmox_sys::fs::{create_path, file_read_optional_string, replace_file, CreateOptions};

use pbsx_api_types::HostAgentStatus;
use pbs_buildcfg::PROXMOX_BACKUP_STATE_DIR_M;
use pbs_config::{open_backup_lockfile, BackupLockGuard};

pub const HOST_STATUS_BASEDIR: &str = concat!(PROXMOX_BACKUP_STATE_DIR_M!(), "/host-status");

/// A host is shown as online if it has checked in within this many seconds.
pub const HOST_ONLINE_THRESHOLD: i64 = 180;

/// Create the persistent host status directory with backup-user ownership.
pub fn create_host_status_dir() -> Result<(), Error> {
    let backup_user = pbs_config::backup_user()?;
    let mode = Mode::from_bits_truncate(0o0750);
    let options = CreateOptions::new()
        .perm(mode)
        .owner(backup_user.uid)
        .group(backup_user.gid);

    create_path(
        HOST_STATUS_BASEDIR,
        Some(options.clone()),
        Some(options),
    )
    .map_err(|err: Error| format_err!("unable to create PBS-X host status directory - {err}"))?;

    Ok(())
}

fn status_path(host: &str) -> PathBuf {
    let mut path = PathBuf::from(HOST_STATUS_BASEDIR);
    path.push(format!("{host}.json"));
    path
}

fn get_lock(path: &Path) -> Result<BackupLockGuard, Error> {
    let mut lock_path = path.to_path_buf();
    lock_path.set_extension("lck");
    open_backup_lockfile(lock_path, None, true)
}

/// Read the latest status for a host.
pub fn read_host_status(host: &str) -> Result<Option<HostAgentStatus>, Error> {
    let path = status_path(host);
    let raw = match file_read_optional_string(&path)? {
        Some(raw) => raw,
        None => return Ok(None),
    };

    serde_json::from_str(&raw)
        .map(Some)
        .map_err(|err| format_err!("unable to parse status for host '{host}' - {err}"))
}

/// Atomically persist a host heartbeat.
pub fn write_host_status(host: &str, status: &HostAgentStatus) -> Result<(), Error> {
    create_host_status_dir()?;

    let path = status_path(host);
    let _lock = get_lock(&path)?;
    let raw = serde_json::to_vec(status)?;

    let backup_user = pbs_config::backup_user()?;
    let options = CreateOptions::new()
        .perm(Mode::from_bits_truncate(0o0640))
        .owner(backup_user.uid)
        .group(backup_user.gid);

    replace_file(&path, &raw, options, true)
        .map_err(|err| format_err!("unable to persist status for host '{host}' - {err}"))?;
    Ok(())
}

/// Remove runtime state after deleting a host definition.
pub fn remove_host_status(host: &str) -> Result<(), Error> {
    let path = status_path(host);
    let _lock = get_lock(&path)?;

    match std::fs::remove_file(&path) {
        Ok(()) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }

    let mut lock_path = path;
    lock_path.set_extension("lck");
    match std::fs::remove_file(lock_path) {
        Ok(()) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }

    Ok(())
}

/// Decide whether a persisted status still represents a connected agent.
pub fn status_is_online(status: &HostAgentStatus, now: i64) -> bool {
    now.saturating_sub(status.last_seen) <= HOST_ONLINE_THRESHOLD
}
