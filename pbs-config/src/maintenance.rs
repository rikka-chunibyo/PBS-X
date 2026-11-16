//! Coordinate native host dispatch with the PBS-X APT pre-install hook.
//!
//! Never unlink the lock file: all processes must keep locking the same inode.
use std::path::Path;
use std::time::Duration;

use anyhow::{Error, bail, format_err};

use crate::{BackupLockGuard, open_backup_lockfile};

pub const HOST_DISPATCH_LOCK: &str = "/etc/proxmox-backup/.pbsx-maintenance.lck";
pub const UPDATE_STATE: &str = "/run/pbsx-repository-update.json";

/// Hold a shared lock through a native queue/claim mutation. The update hook
/// takes the exclusive lock before its idle check and keeps it until shutdown
/// and archiving finish. The state marker also blocks dispatch if a package's
/// postinst temporarily starts the server before the APT post hook runs.
pub fn lock_host_dispatch() -> Result<BackupLockGuard, Error> {
    lock_host_dispatch_at(Path::new(HOST_DISPATCH_LOCK), Path::new(UPDATE_STATE))
}

fn lock_host_dispatch_at(lock: &Path, state: &Path) -> Result<BackupLockGuard, Error> {
    let guard = open_backup_lockfile(lock, Some(Duration::ZERO), false).map_err(|err| {
        format_err!("PBS-X host dispatch is unavailable during maintenance: {err}")
    })?;
    if state.try_exists()? {
        bail!("PBS-X host dispatch is paused for a package transaction");
    }
    Ok(guard)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::process::Command;
    use std::time::SystemTime;

    #[test]
    fn dispatch_lock_interoperates_with_apt_and_honors_transaction_state() {
        let user = nix::unistd::User::from_uid(nix::unistd::Uid::current())
            .unwrap()
            .unwrap();
        proxmox_product_config::init(user.clone(), user);
        let unique = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        let root =
            std::env::temp_dir().join(format!("pbsx-maintenance-{}-{unique}", std::process::id()));
        std::fs::create_dir(&root).unwrap();
        let lock = root.join("maintenance.lck");
        let state = root.join("transaction.json");

        let guard = lock_host_dispatch_at(&lock, &state).unwrap();
        // Exercise the actual Python flock used by the APT hook, not another
        // instance of our Rust helper. It must fail while dispatch holds SH.
        let output = Command::new("python3").arg("-c").arg(
            "import fcntl,sys\nf=open(sys.argv[1], 'r+')\ntry: fcntl.flock(f, fcntl.LOCK_EX|fcntl.LOCK_NB)\nexcept BlockingIOError: sys.exit(2)"
        ).arg(&lock).output().unwrap();
        assert_eq!(output.status.code(), Some(2));
        drop(guard);
        let exclusive = open_backup_lockfile(&lock, Some(Duration::ZERO), true).unwrap();
        assert!(lock_host_dispatch_at(&lock, &state).is_err());
        drop(exclusive);

        // The marker continues to block work after the pre-hook exits, even
        // if dpkg temporarily starts a newly installed server.
        std::fs::write(&state, b"{}").unwrap();
        assert!(lock_host_dispatch_at(&lock, &state).is_err());
        std::fs::remove_file(&state).unwrap();
        drop(lock_host_dispatch_at(&lock, &state).unwrap());
        std::fs::remove_dir_all(root).unwrap();
    }
}
