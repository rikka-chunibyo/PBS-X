//! Runtime state for PBS-X protected hosts.
//!
//! Static host definitions live in `/etc/proxmox-backup/host.cfg`. Agent heartbeats are written
//! separately under the PBS state directory so a heartbeat does not constantly mutate the admin
//! configuration or its digest.

use std::path::{Path, PathBuf};

use anyhow::{Error, bail, format_err};
use nix::sys::stat::Mode;

use proxmox_sys::fs::{CreateOptions, create_path, file_read_optional_string, replace_file};

use pbs_api_types::{Authid, Tokenname, Userid};
use pbs_buildcfg::PROXMOX_BACKUP_STATE_DIR_M;
use pbs_config::{BackupLockGuard, open_backup_lockfile};
use pbsx_api_types::{
    HostAgentStatus, HostBackupJobConfig, HostBackupRunCommand, HostBackupRunInfo,
    HostBackupRunProgress, HostBackupRunResult, HostBackupRunState, HostConfig,
    HostEnrollmentToken, HostRestoreMode, HostRestoreRunCommand, HostRestoreRunInfo,
    HostRestoreRunProgress, HostRestoreRunResult, HostRestoreRunState,
};

pub const HOST_STATUS_BASEDIR: &str = concat!(PROXMOX_BACKUP_STATE_DIR_M!(), "/host-status");
pub const HOST_ENROLLMENT_BASEDIR: &str =
    concat!(PROXMOX_BACKUP_STATE_DIR_M!(), "/pbsx-enrollment");
pub const HOST_CREDENTIAL_BASEDIR: &str =
    concat!(PROXMOX_BACKUP_STATE_DIR_M!(), "/pbsx-agent-credentials");
pub const HOST_RUN_BASEDIR: &str = concat!(PROXMOX_BACKUP_STATE_DIR_M!(), "/pbsx-host-runs");

const HOST_ENROLLMENT_TTL: i64 = 15 * 60;

#[derive(serde::Deserialize, serde::Serialize)]
struct HostEnrollmentRecord {
    digest: String,
    expires: i64,
    #[serde(default)]
    revoke_existing_keys: bool,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
pub struct HostAgentCredentialRecord {
    pub certificate_fingerprint: String,
    pub backup_auth_id: String,
    pub issued: i64,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub previous_backup_auth_ids: Vec<String>,
    // Keep historical owners so revoking a key does not orphan its snapshots.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub retired_backup_auth_ids: Vec<String>,
    // Only these automatically managed grants are removed on a job edit.
    // Namespace grants added manually through PBS are preserved.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub managed_upload_paths: Option<Vec<String>>,
}

impl HostAgentCredentialRecord {
    fn active_auth_ids(&self) -> impl Iterator<Item = &str> {
        std::iter::once(self.backup_auth_id.as_str())
            .chain(self.previous_backup_auth_ids.iter().map(String::as_str))
    }

    pub(crate) fn known_owner(&self, auth_id: &str) -> bool {
        self.active_auth_ids()
            .chain(self.retired_backup_auth_ids.iter().map(String::as_str))
            .any(|id| id == auth_id)
    }

    fn successor(
        previous: Option<&Self>,
        auth_id: String,
        fingerprint: String,
        revoke: bool,
    ) -> Self {
        let mut record = Self {
            certificate_fingerprint: fingerprint,
            backup_auth_id: auth_id,
            issued: proxmox_time::epoch_i64(),
            previous_backup_auth_ids: Vec::new(),
            retired_backup_auth_ids: previous
                .map(|p| p.retired_backup_auth_ids.clone())
                .unwrap_or_default(),
            managed_upload_paths: previous.and_then(|p| p.managed_upload_paths.clone()),
        };
        if let Some(previous) = previous {
            let ids = previous.active_auth_ids().map(str::to_owned);
            if revoke {
                record.retired_backup_auth_ids.extend(ids);
            } else {
                record.previous_backup_auth_ids.extend(ids);
            }
        }
        record.retired_backup_auth_ids.sort();
        record.retired_backup_auth_ids.dedup();
        record
    }
}

#[cfg(test)]
mod enrollment_key_tests {
    use super::*;

    #[test]
    fn automatic_acl_sync_preserves_manual_namespaces_and_roles() {
        let id: Authid = "root@pam!phone".parse().unwrap();
        let mut acl = pbs_config::acl::AclTree::default();
        acl.insert_user_role(
            "/datastore/backup/old-device",
            &id,
            "DatastoreBackup",
            false,
        );
        acl.insert_user_role("/datastore/backup/shared", &id, "DatastoreBackup", false);
        acl.insert_user_role(
            "/datastore/backup/old-device",
            &id,
            "DatastoreReader",
            false,
        );
        update_managed_upload_acl(
            &mut acl,
            "phone",
            &id,
            false,
            &["/datastore/backup/old-device".into()],
            &["/datastore/backup/devices/phone".into()],
        );
        assert!(
            !acl.find_node("/datastore/backup/old-device").unwrap().users[&id]
                .contains_key("DatastoreBackup")
        );
        assert!(
            acl.find_node("/datastore/backup/old-device").unwrap().users[&id]
                .contains_key("DatastoreReader")
        );
        assert!(
            acl.find_node("/datastore/backup/shared").unwrap().users[&id]
                .contains_key("DatastoreBackup")
        );
        assert!(
            acl.find_node("/datastore/backup/devices/phone")
                .unwrap()
                .users[&id]
                .contains_key("DatastoreBackup")
        );
    }

    #[test]
    fn administrator_widened_propagation_is_not_reset_by_job_edits() {
        let id: Authid = "root@pam!phone".parse().unwrap();
        let mut acl = pbs_config::acl::AclTree::default();
        let paths: Vec<String> = vec!["/datastore/backup/devices/phone".into()];
        acl.insert_user_role(&paths[0], &id, "DatastoreBackup", true);
        update_managed_upload_acl(&mut acl, "phone", &id, true, &paths, &paths);
        assert_eq!(
            acl.find_node(&paths[0]).unwrap().users[&id]["DatastoreBackup"],
            true
        );
        update_managed_upload_acl(&mut acl, "phone", &id, false, &paths, &[]);
        assert_eq!(
            acl.find_node(&paths[0]).unwrap().users[&id]["DatastoreBackup"],
            true
        );
        assert!(!acl.find_node("/host/phone").unwrap().users[&id].contains_key("HostAgent"));
    }

    #[test]
    fn legacy_records_preserve_existing_key_by_default() {
        let grant: HostEnrollmentRecord =
            serde_json::from_str(r#"{"digest":"hash","expires":42}"#).unwrap();
        assert!(!grant.revoke_existing_keys);
        let old: HostAgentCredentialRecord = serde_json::from_str(
            r#"{"certificate-fingerprint":"pin","backup-auth-id":"root@pam!legacy","issued":1}"#,
        )
        .unwrap();
        let next = HostAgentCredentialRecord::successor(
            Some(&old),
            "root@pam!second".into(),
            "pin2".into(),
            false,
        );
        assert_eq!(next.previous_backup_auth_ids, ["root@pam!legacy"]);
        assert!(next.retired_backup_auth_ids.is_empty());
        assert!(next.known_owner("root@pam!legacy"));
        assert!(!next.known_owner("root@pam!manual"));
    }

    #[test]
    fn revocation_keeps_historical_owners_without_active_access() {
        let a = HostAgentCredentialRecord::successor(
            None,
            "root@pam!first".into(),
            "pin".into(),
            false,
        );
        let b = HostAgentCredentialRecord::successor(
            Some(&a),
            "root@pam!second".into(),
            "pin".into(),
            false,
        );
        let c = HostAgentCredentialRecord::successor(
            Some(&b),
            "root@pam!third".into(),
            "pin".into(),
            true,
        );
        assert_eq!(c.active_auth_ids().collect::<Vec<_>>(), ["root@pam!third"]);
        assert_eq!(
            c.retired_backup_auth_ids,
            ["root@pam!first", "root@pam!second"]
        );
        let d = HostAgentCredentialRecord::successor(
            Some(&c),
            "root@pam!fourth".into(),
            "pin".into(),
            false,
        );
        assert_eq!(d.previous_backup_auth_ids, ["root@pam!third"]);
        assert!(d.known_owner("root@pam!first"));
        assert!(!d.active_auth_ids().any(|id| id == "root@pam!first"));
    }

    #[test]
    fn new_token_names_are_distinct_and_valid() {
        let a = token_name_for_host("fixture-host").unwrap();
        let b = token_name_for_host("fixture-host").unwrap();
        assert_ne!(a, b);
        // user.cfg limits the complete section identifier, including user@realm!,
        // rather than only the Tokenname accepted by the API type.
        assert!(
            Authid::from((Userid::root_userid().clone(), Some(a)))
                .to_string()
                .len()
                <= 64
        );
    }
}

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

    create_path(HOST_STATUS_BASEDIR, Some(options), Some(options)).map_err(|err: Error| {
        format_err!("unable to create PBS-X host status directory - {err}")
    })?;

    create_path(HOST_ENROLLMENT_BASEDIR, Some(options), Some(options))
        .map_err(|err: Error| format_err!("unable to create PBS-X enrollment directory - {err}"))?;

    create_path(HOST_CREDENTIAL_BASEDIR, Some(options), Some(options))
        .map_err(|err: Error| format_err!("unable to create PBS-X credential directory - {err}"))?;

    create_path(HOST_RUN_BASEDIR, Some(options), Some(options))
        .map_err(|err: Error| format_err!("unable to create PBS-X host run directory - {err}"))?;

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

fn enrollment_path(host: &str) -> PathBuf {
    let mut path = PathBuf::from(HOST_ENROLLMENT_BASEDIR);
    path.push(format!("{host}.json"));
    path
}

/// Create a new short-lived, one-time enrollment credential. Only its SHA-256 digest is stored.
pub fn create_enrollment_token(
    host: &str,
    revoke_existing_keys: bool,
) -> Result<HostEnrollmentToken, Error> {
    create_host_status_dir()?;

    let mut raw = [0u8; 32];
    proxmox_sys::linux::fill_with_random_data(&mut raw)?;
    let token = hex::encode(raw);
    let expires = proxmox_time::epoch_i64() + HOST_ENROLLMENT_TTL;
    let record = HostEnrollmentRecord {
        digest: hex::encode(openssl::sha::sha256(token.as_bytes())),
        expires,
        revoke_existing_keys,
    };

    let path = enrollment_path(host);
    let _lock = get_lock(&path)?;
    let raw = serde_json::to_vec(&record)?;
    let backup_user = pbs_config::backup_user()?;
    let options = CreateOptions::new()
        .perm(Mode::from_bits_truncate(0o0640))
        .owner(backup_user.uid)
        .group(backup_user.gid);
    replace_file(&path, &raw, options, true).map_err(|err| {
        format_err!("unable to persist enrollment token for host '{host}' - {err}")
    })?;

    Ok(HostEnrollmentToken {
        token,
        expires,
        server_fingerprint: crate::cert_info()?.fingerprint()?,
    })
}

fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    let mut diff = 0u8;
    for (&left, &right) in a.iter().zip(b.iter()) {
        diff |= left ^ right;
    }
    diff == 0
}

/// Validate and consume a one-time enrollment token. A successful check always removes it.
pub fn consume_enrollment_token(host: &str, token: &str) -> Result<bool, Error> {
    let path = enrollment_path(host);
    let _lock = get_lock(&path)?;
    let raw = file_read_optional_string(&path)?
        .ok_or_else(|| format_err!("no enrollment token exists for host '{host}'"))?;
    let record: HostEnrollmentRecord = serde_json::from_str(&raw)
        .map_err(|err| format_err!("unable to parse enrollment token for host '{host}' - {err}"))?;

    if record.expires < proxmox_time::epoch_i64() {
        let _ = std::fs::remove_file(&path);
        bail!("enrollment token for host '{host}' has expired");
    }

    let supplied = hex::encode(openssl::sha::sha256(token.as_bytes()));
    if !constant_time_eq(record.digest.as_bytes(), supplied.as_bytes()) {
        bail!("invalid enrollment token for host '{host}'");
    }

    std::fs::remove_file(&path).map_err(|err| {
        format_err!("unable to consume enrollment token for host '{host}' - {err}")
    })?;
    Ok(record.revoke_existing_keys)
}

fn credential_path(host: &str) -> PathBuf {
    let mut path = PathBuf::from(HOST_CREDENTIAL_BASEDIR);
    path.push(format!("{host}.json"));
    path
}

pub fn read_host_credentials(host: &str) -> Result<Option<HostAgentCredentialRecord>, Error> {
    let path = credential_path(host);
    let Some(raw) = file_read_optional_string(&path)? else {
        return Ok(None);
    };
    serde_json::from_str(&raw)
        .map(Some)
        .map_err(|err| format_err!("unable to parse credentials for host '{host}' - {err}"))
}

// Caller holds the host credential lock through issuance, commit and ACL changes.
fn write_host_credentials(host: &str, record: &HostAgentCredentialRecord) -> Result<(), Error> {
    create_host_status_dir()?;
    let path = credential_path(host);
    let raw = serde_json::to_vec(record)?;
    let backup_user = pbs_config::backup_user()?;
    let options = CreateOptions::new()
        .perm(Mode::from_bits_truncate(0o0640))
        .owner(backup_user.uid)
        .group(backup_user.gid);
    replace_file(&path, &raw, options, true)
        .map_err(|err| format_err!("unable to persist credentials for host '{host}' - {err}"))
}

/// Return the host bound to a PBS upload token, if it is a PBS-X agent token.
pub fn host_for_upload_auth(auth_id: &Authid) -> Result<Option<String>, Error> {
    if !auth_id.is_token() {
        return Ok(None);
    }
    let entries = match std::fs::read_dir(HOST_CREDENTIAL_BASEDIR) {
        Ok(entries) => entries,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(err) => return Err(err.into()),
    };
    let wanted = auth_id.to_string();
    for entry in entries {
        let entry = entry?;
        if entry.path().extension().and_then(|ext| ext.to_str()) != Some("json") {
            continue;
        }
        let raw = std::fs::read_to_string(entry.path())?;
        let record: HostAgentCredentialRecord = serde_json::from_str(&raw)?;
        if record.known_owner(&wanted) {
            if !record.active_auth_ids().any(|id| id == wanted) {
                bail!("PBS-X generated key has been revoked");
            }
            if let Some(stem) = entry.path().file_stem().and_then(|stem| stem.to_str()) {
                return Ok(Some(stem.to_string()));
            }
        }
    }
    Ok(None)
}

fn token_name_for_host(host: &str) -> Result<Tokenname, Error> {
    let digest = hex::encode(openssl::sha::sha256(host.as_bytes()));
    let mut nonce = [0u8; 16];
    proxmox_sys::linux::fill_with_random_data(&mut nonce)?;
    Tokenname::try_from(format!("pbsx-{}-{}", &digest[..16], hex::encode(nonce)))
}

/// Keep normal PBS ownership checks for ordinary keys. Generated keys for one
/// device can also access that device's historical generated owners. Callers
/// enforce ordinary namespace ACLs first; no group or owner is renamed.
pub fn check_host_backup_owner(
    owner: &Authid,
    auth_id: &Authid,
    _store: &str,
    _ns: &pbs_api_types::BackupNamespace,
    _group: &pbs_api_types::BackupGroup,
) -> Result<(), Error> {
    if let Some(host) = host_for_upload_auth(auth_id)? {
        if let Some(record) = read_host_credentials(&host)? {
            if record.known_owner(&owner.to_string()) {
                return Ok(());
            }
        }
    }
    pbs_datastore::check_backup_owner(owner, auth_id)
}

fn host_upload_paths(host: &str) -> Result<Vec<String>, Error> {
    let (config, _) = pbs_config::host_backup::config()?;
    let jobs: Vec<HostBackupJobConfig> = config.convert_to_typed_array("host-backup")?;
    let mut paths: Vec<_> = jobs
        .into_iter()
        .filter(|job| job.enable && job.host == host)
        .map(|job| format!("/{}", job.datastore_acl_path().join("/")))
        .collect();
    paths.sort();
    paths.dedup();
    Ok(paths)
}

/// Snapshot legacy automatic grants before changing the job configuration.
/// This avoids treating any unrelated administrator-added ACL as managed.
pub fn initialize_host_upload_acl_tracking(host: &str) -> Result<(), Error> {
    create_host_status_dir()?;
    let _lock = get_lock(&credential_path(host))?;
    if let Some(mut record) = read_host_credentials(host)? {
        if record.managed_upload_paths.is_none() {
            record.managed_upload_paths = Some(host_upload_paths(host)?);
            write_host_credentials(host, &record)?;
        }
    }
    Ok(())
}

fn update_managed_upload_acl(
    acl: &mut pbs_config::acl::AclTree,
    host: &str,
    auth_id: &Authid,
    controller: bool,
    previous: &[String],
    desired: &[String],
) {
    acl.delete_user_role(&format!("/host/{host}"), auth_id, "HostAgent");
    if controller {
        acl.insert_user_role(&format!("/host/{host}"), auth_id, "HostAgent", false);
    }
    for path in previous.iter().filter(|path| !desired.contains(path)) {
        // A propagated grant was explicitly widened by an administrator.
        let managed = acl
            .find_node(path)
            .and_then(|node| node.users.get(auth_id))
            .and_then(|roles| roles.get("DatastoreBackup"))
            == Some(&false);
        if managed {
            acl.delete_user_role(path, auth_id, "DatastoreBackup");
        }
    }
    for path in desired {
        let exists = acl
            .find_node(path)
            .and_then(|node| node.users.get(auth_id))
            .is_some_and(|roles| roles.contains_key("DatastoreBackup"));
        if !exists {
            acl.insert_user_role(path, auth_id, "DatastoreBackup", false);
        }
    }
}

fn sync_host_upload_acl_for(
    host: &str,
    auth_id: &Authid,
    controller: bool,
    previous: &[String],
) -> Result<Vec<String>, Error> {
    let desired = host_upload_paths(host)?;
    let _lock = pbs_config::acl::lock_config()?;
    let (mut acl, _) = pbs_config::acl::config()?;
    update_managed_upload_acl(&mut acl, host, auth_id, controller, previous, &desired);
    pbs_config::acl::save_config(&acl)?;
    Ok(desired)
}

/// Refresh datastore/host ACLs for an already enrolled host after its job configuration changes.
pub fn sync_host_upload_acl(host: &str) -> Result<(), Error> {
    create_host_status_dir()?;
    let _lock = get_lock(&credential_path(host))?;
    let Some(mut record) = read_host_credentials(host)? else {
        return Ok(());
    };
    for id in record.active_auth_ids() {
        // Administrators may revoke individual generated tokens through PBS.
        let (users, _) = pbs_config::user::config()?;
        if users.sections.contains_key(id) {
            sync_host_upload_acl_for(
                host,
                &id.parse()?,
                id == record.backup_auth_id,
                record.managed_upload_paths.as_deref().unwrap_or(&[]),
            )?;
        }
    }
    record.managed_upload_paths = Some(host_upload_paths(host)?);
    write_host_credentials(host, &record)
}

/// Revoke a generated host upload token and remove any ACL entries for it.
/// This is used as rollback when enrollment cannot commit all credential state.
pub fn revoke_host_upload_auth(auth_id: &str) -> Result<(), Error> {
    let auth_id: Authid = auth_id.parse()?;
    if let Some(token_name) = auth_id.tokenname().map(ToOwned::to_owned) {
        let (users, _) = pbs_config::user::config()?;
        if users.sections.contains_key(&auth_id.to_string()) {
            crate::api2::access::user::delete_token(auth_id.user().clone(), token_name, None)?;
        } else {
            // Token generation writes its shadow secret before user.cfg. Clean
            // up that secret as well if generation/config persistence failed.
            pbs_config::token_shadow::delete_secret(&auth_id)?;
        }
    }

    let _lock = pbs_config::acl::lock_config()?;
    let (mut acl, _) = pbs_config::acl::config()?;
    acl.delete_authid(&auth_id);
    pbs_config::acl::save_config(&acl)?;
    Ok(())
}

/// Issue an independent token; never replace another enrollment's secret.
fn issue_host_upload_token(host: &str) -> Result<(String, String), Error> {
    let userid = Userid::root_userid().clone();
    let token_name = token_name_for_host(host)?;
    let token_id = Authid::from((userid.clone(), Some(token_name.clone())));
    let value = match crate::api2::access::user::generate_token(
        userid,
        token_name,
        Some(format!("PBS-X host agent: {host}")),
        Some(true),
        None,
        None,
    ) {
        Ok(value) => value,
        Err(err) => {
            let _ = revoke_host_upload_auth(&token_id.to_string());
            return Err(err);
        }
    };
    let auth_id = value["tokenid"]
        .as_str()
        .ok_or_else(|| format_err!("generated token response has no tokenid"))?
        .parse::<Authid>()?;
    let secret = value["value"]
        .as_str()
        .ok_or_else(|| format_err!("generated token response has no secret"))?
        .to_string();

    if let Err(err) = sync_host_upload_acl_for(host, &auth_id, false, &[]) {
        let auth_id_string = auth_id.to_string();
        let _ = revoke_host_upload_auth(&auth_id_string);
        return Err(err);
    }
    Ok((auth_id.to_string(), secret))
}

/// Redeem the server-side grant under one host lock. Issuing or abandoning a
/// grant never revokes keys. The choice cannot be changed by an enrolling device.
pub fn enroll_host(
    host: &str,
    token: &str,
    fingerprint: String,
) -> Result<(String, String), Error> {
    create_host_status_dir()?;
    let _lock = get_lock(&credential_path(host))?;
    let revoke = consume_enrollment_token(host, token)?;
    let previous = read_host_credentials(host)?;
    let (auth_id, secret) = issue_host_upload_token(host)?;
    let mut record = HostAgentCredentialRecord::successor(
        previous.as_ref(),
        auth_id.clone(),
        fingerprint,
        revoke,
    );
    if let Err(err) = write_host_credentials(host, &record) {
        let _ = revoke_host_upload_auth(&auth_id);
        return Err(err);
    }
    // Persist the complete registry before irreversible revocation. On failure
    // it still records every issued ID, allowing a subsequent enrollment/delete
    // to clean up; secrets are never persisted here.
    if revoke {
        for id in &record.retired_backup_auth_ids {
            revoke_host_upload_auth(id)?;
        }
    }
    for id in record.active_auth_ids() {
        let (users, _) = pbs_config::user::config()?;
        if users.sections.contains_key(id) {
            sync_host_upload_acl_for(
                host,
                &id.parse()?,
                id == record.backup_auth_id,
                record.managed_upload_paths.as_deref().unwrap_or(&[]),
            )?;
        }
    }
    record.managed_upload_paths = Some(host_upload_paths(host)?);
    write_host_credentials(host, &record)?;
    Ok((auth_id, secret))
}

/// Remove the agent upload token and stored credential fingerprint for a host.
pub fn remove_host_credentials(host: &str) -> Result<(), Error> {
    create_host_status_dir()?;
    let path = credential_path(host);
    let _lock = get_lock(&path)?;
    if let Some(record) = read_host_credentials(host)? {
        for id in record
            .active_auth_ids()
            .chain(record.retired_backup_auth_ids.iter().map(String::as_str))
        {
            revoke_host_upload_auth(id)?;
        }
    }
    match std::fs::remove_file(path) {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(err) => Err(err.into()),
    }
}

/// Remove any outstanding enrollment credential for a host.
pub fn remove_enrollment_token(host: &str) -> Result<(), Error> {
    let path = enrollment_path(host);
    let _lock = get_lock(&path)?;
    match std::fs::remove_file(&path) {
        Ok(()) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }
    Ok(())
}

/// Remove runtime state after deleting a host definition.
pub fn remove_host_status(host: &str) -> Result<(), Error> {
    remove_enrollment_token(host)?;
    remove_host_credentials(host)?;
    remove_host_runs(host)?;
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

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
struct HostBackupRunProgressRecord {
    progress: HostBackupRunProgress,
    started_at: i64,
    updated_at: i64,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
struct HostBackupRunHistory {
    command: HostBackupRunCommand,
    result: HostBackupRunResult,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    progress: Option<HostBackupRunProgressRecord>,
    finished_at: i64,
}

fn run_host_dir(host: &str) -> PathBuf {
    let mut path = PathBuf::from(HOST_RUN_BASEDIR);
    path.push(host);
    path
}

fn create_run_dirs(host: &str) -> Result<(), Error> {
    create_host_status_dir()?;
    let backup_user = pbs_config::backup_user()?;
    let options = CreateOptions::new()
        .perm(Mode::from_bits_truncate(0o0750))
        .owner(backup_user.uid)
        .group(backup_user.gid);
    let host_dir = run_host_dir(host);
    create_path(&host_dir, Some(options), Some(options))?;
    for child in [
        "queued",
        "running",
        "progress",
        "history",
        "restore-queued",
        "restore-running",
        "restore-progress",
        "restore-history",
    ] {
        create_path(host_dir.join(child), Some(options), Some(options))?;
    }
    Ok(())
}

fn host_run_lock(host: &str) -> Result<BackupLockGuard, Error> {
    create_run_dirs(host)?;
    open_backup_lockfile(run_host_dir(host).join(".lock"), None, true)
}

fn write_run_json<T: serde::Serialize>(path: &Path, value: &T) -> Result<(), Error> {
    let backup_user = pbs_config::backup_user()?;
    let options = CreateOptions::new()
        .perm(Mode::from_bits_truncate(0o0640))
        .owner(backup_user.uid)
        .group(backup_user.gid);
    let raw = serde_json::to_vec_pretty(value)?;
    replace_file(path, &raw, options, true)?;
    Ok(())
}

fn read_run_command(path: &Path) -> Result<HostBackupRunCommand, Error> {
    let raw = std::fs::read(path)?;
    serde_json::from_slice(&raw)
        .map_err(|err| format_err!("unable to parse PBS-X host run '{}': {err}", path.display()))
}

fn read_run_progress(path: &Path) -> Result<HostBackupRunProgressRecord, Error> {
    let raw = std::fs::read(path)?;
    serde_json::from_slice(&raw).map_err(|err| {
        format_err!(
            "unable to parse PBS-X host run progress '{}': {err}",
            path.display()
        )
    })
}

fn read_run_history(path: &Path) -> Result<HostBackupRunHistory, Error> {
    let raw = std::fs::read(path)?;
    serde_json::from_slice(&raw).map_err(|err| {
        format_err!(
            "unable to parse PBS-X host run history '{}': {err}",
            path.display()
        )
    })
}

fn run_progress_path(host: &str, run_id: &str) -> PathBuf {
    run_host_dir(host)
        .join("progress")
        .join(format!("{run_id}.json"))
}

/// Queue one configured host backup job for pickup by the enrolled agent.
pub fn queue_host_backup(job: &HostBackupJobConfig) -> Result<HostBackupRunCommand, Error> {
    let (host_config, _) = pbs_config::host::config()?;
    let host: HostConfig = host_config
        .lookup("host", &job.host)
        .map_err(|_| format_err!("PBS-X host '{}' does not exist", job.host))?;
    if !host.enable {
        bail!("PBS-X host '{}' is disabled", job.host);
    }

    create_run_dirs(&job.host)?;
    let _lock = host_run_lock(&job.host)?;

    // Coalesce duplicate schedule/manual requests for the same job while the host is offline or
    // still processing it. We want one current recovery point, not a backlog of missed intervals.
    let base = run_host_dir(&job.host);
    for state in ["running", "queued"] {
        let dir = base.join(state);
        if let Ok(entries) = std::fs::read_dir(&dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|ext| ext.to_str()) != Some("json") {
                    continue;
                }
                if let Ok(existing) = read_run_command(&path) {
                    if existing.job_id == job.id {
                        return Ok(existing);
                    }
                }
            }
        }
    }

    let mut random = [0u8; 12];
    proxmox_sys::linux::fill_with_random_data(&mut random)?;
    let requested_at = proxmox_time::epoch_i64();

    // Assign backup time on the PBS-X server rather than trusting the agent clock. Keep it
    // monotonic across queued/running work for this host and stable across agent retries.
    let mut backup_time = requested_at;
    for state in ["running", "queued"] {
        let dir = base.join(state);
        if let Ok(entries) = std::fs::read_dir(&dir) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|ext| ext.to_str()) != Some("json") {
                    continue;
                }
                if let Ok(existing) = read_run_command(&path) {
                    backup_time = backup_time.max(existing.backup_time.saturating_add(1));
                }
            }
        }
    }
    if let Ok(entries) = std::fs::read_dir(base.join("history")) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|ext| ext.to_str()) != Some("json") {
                continue;
            }
            if let Ok(existing) = read_run_history(&path) {
                backup_time = backup_time.max(existing.command.backup_time.saturating_add(1));
            }
        }
    }

    let run_id = format!("{}-{}", requested_at, hex::encode(random));
    let command = HostBackupRunCommand {
        run_id: run_id.clone(),
        job_id: job.id.clone(),
        host_id: job.host.clone(),
        store: job.store.clone(),
        ns: job.ns.clone(),
        sources: job.sources.clone(),
        excludes: job.excludes.clone(),
        snapshot_mode: job.snapshot_mode,
        change_detection_mode: job.change_detection_mode,
        requested_at,
        backup_time,
    };
    let path = run_host_dir(&job.host)
        .join("queued")
        .join(format!("{run_id}.json"));
    write_run_json(&path, &command)?;
    Ok(command)
}

fn first_json_file(path: &Path) -> Result<Option<PathBuf>, Error> {
    let mut files = match std::fs::read_dir(path) {
        Ok(entries) => entries
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .filter(|path| path.extension().and_then(|v| v.to_str()) == Some("json"))
            .collect::<Vec<_>>(),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(err) => return Err(err.into()),
    };
    files.sort();
    Ok(files.into_iter().next())
}

/// Atomically claim the next run for a host. An existing running command is returned again so an
/// agent restart can recover a run whose HTTP response or process was lost.
pub fn claim_host_backup(host: &str) -> Result<Option<HostBackupRunCommand>, Error> {
    create_run_dirs(host)?;
    let _lock = host_run_lock(host)?;
    let base = run_host_dir(host);
    if let Some(path) = first_json_file(&base.join("running"))? {
        let command = read_run_command(&path)?;
        ensure_initial_run_progress(&command)?;
        return Ok(Some(command));
    }
    let Some(queued) = first_json_file(&base.join("queued"))? else {
        return Ok(None);
    };
    let file_name = queued
        .file_name()
        .ok_or_else(|| format_err!("invalid queued host run path"))?;
    let running = base.join("running").join(file_name);
    std::fs::rename(&queued, &running)?;
    let command = read_run_command(&running)?;
    ensure_initial_run_progress(&command)?;
    Ok(Some(command))
}

fn ensure_initial_run_progress(command: &HostBackupRunCommand) -> Result<(), Error> {
    let path = run_progress_path(&command.host_id, &command.run_id);
    if path.exists() {
        return Ok(());
    }
    let now = proxmox_time::epoch_i64();
    let record = HostBackupRunProgressRecord {
        progress: HostBackupRunProgress {
            host_id: command.host_id.clone(),
            run_id: command.run_id.clone(),
            stage: Some("running".to_string()),
            ..Default::default()
        },
        started_at: now,
        updated_at: now,
    };
    write_run_json(&path, &record)
}

/// Persist agent progress independently from the immutable claimed command.
pub fn update_host_backup_progress(progress: &HostBackupRunProgress) -> Result<(), Error> {
    create_run_dirs(&progress.host_id)?;
    let _lock = host_run_lock(&progress.host_id)?;
    let base = run_host_dir(&progress.host_id);
    let running = base
        .join("running")
        .join(format!("{}.json", progress.run_id));
    let command = read_run_command(&running)
        .map_err(|_| format_err!("host run '{}' is not currently claimed", progress.run_id))?;
    if command.host_id != progress.host_id || command.run_id != progress.run_id {
        bail!("host run progress identity mismatch");
    }

    let path = run_progress_path(&progress.host_id, &progress.run_id);
    let now = proxmox_time::epoch_i64();
    let previous = read_run_progress(&path).ok();
    let started_at = previous
        .as_ref()
        .map(|record| record.started_at)
        .unwrap_or(now);
    let mut current = progress.clone();
    if let Some(previous) = previous {
        current.processed_bytes = current
            .processed_bytes
            .max(previous.progress.processed_bytes);
        current.uploaded_bytes = current.uploaded_bytes.max(previous.progress.uploaded_bytes);
        current.processed_chunks = current
            .processed_chunks
            .max(previous.progress.processed_chunks);
        current.uploaded_chunks = current
            .uploaded_chunks
            .max(previous.progress.uploaded_chunks);
        current.files = current.files.max(previous.progress.files);
        if current.stage.is_none() {
            current.stage = previous.progress.stage;
        }
    }
    write_run_json(
        &path,
        &HostBackupRunProgressRecord {
            progress: current,
            started_at,
            updated_at: now,
        },
    )
}

/// Complete a claimed run and move it into immutable history.
pub fn finish_host_backup(result: &HostBackupRunResult) -> Result<(), Error> {
    create_run_dirs(&result.host_id)?;
    let _lock = host_run_lock(&result.host_id)?;
    let base = run_host_dir(&result.host_id);
    let running = base.join("running").join(format!("{}.json", result.run_id));
    let command = read_run_command(&running)
        .map_err(|_| format_err!("host run '{}' is not currently claimed", result.run_id))?;
    if command.host_id != result.host_id {
        bail!("host run identity mismatch");
    }
    let progress_path = run_progress_path(&result.host_id, &result.run_id);
    let progress = read_run_progress(&progress_path).ok();
    let record = HostBackupRunHistory {
        command,
        result: result.clone(),
        progress,
        finished_at: proxmox_time::epoch_i64(),
    };
    let history = base.join("history").join(format!("{}.json", result.run_id));
    write_run_json(&history, &record)?;
    std::fs::remove_file(running)?;
    let _ = std::fs::remove_file(progress_path);
    prune_host_run_history(&base.join("history"), 200)?;
    Ok(())
}

fn prune_host_run_history(path: &Path, keep: usize) -> Result<(), Error> {
    let mut files = match std::fs::read_dir(path) {
        Ok(entries) => entries
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("json"))
            .collect::<Vec<_>>(),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(err) => return Err(err.into()),
    };
    files.sort();
    let remove = files.len().saturating_sub(keep);
    for path in files.into_iter().take(remove) {
        let _ = std::fs::remove_file(path);
    }
    Ok(())
}

fn run_info_from_command(
    command: &HostBackupRunCommand,
    state: HostBackupRunState,
    progress: Option<&HostBackupRunProgressRecord>,
) -> HostBackupRunInfo {
    let (stage, processed_bytes, uploaded_bytes, processed_chunks, uploaded_chunks, files) =
        progress
            .map(|record| {
                (
                    record.progress.stage.clone(),
                    record.progress.processed_bytes,
                    record.progress.uploaded_bytes,
                    record.progress.processed_chunks,
                    record.progress.uploaded_chunks,
                    record.progress.files,
                )
            })
            .unwrap_or((None, 0, 0, 0, 0, 0));
    HostBackupRunInfo {
        run_id: command.run_id.clone(),
        job_id: command.job_id.clone(),
        host_id: command.host_id.clone(),
        state,
        store: command.store.clone(),
        ns: command.ns.clone(),
        requested_at: command.requested_at,
        backup_time: command.backup_time,
        started_at: progress.map(|record| record.started_at),
        updated_at: progress.map(|record| record.updated_at),
        finished_at: None,
        success: None,
        error: None,
        stage,
        processed_bytes,
        uploaded_bytes,
        processed_chunks,
        uploaded_chunks,
        files,
        directories: 0,
        logical_bytes: 0,
        duration_ms: 0,
    }
}

/// List current and recent host backup orchestration records. Actual backup
/// retention and existence are represented by datastore snapshots, not this log.
pub fn list_host_backup_runs(host: &str) -> Result<Vec<HostBackupRunInfo>, Error> {
    create_run_dirs(host)?;
    let _lock = host_run_lock(host)?;
    let base = run_host_dir(host);
    let mut out = Vec::new();

    for (dir, state) in [
        ("queued", HostBackupRunState::Queued),
        ("running", HostBackupRunState::Running),
    ] {
        if let Ok(entries) = std::fs::read_dir(base.join(dir)) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|value| value.to_str()) != Some("json") {
                    continue;
                }
                let Ok(command) = read_run_command(&path) else {
                    continue;
                };
                let progress = if matches!(state, HostBackupRunState::Running) {
                    read_run_progress(&run_progress_path(host, &command.run_id)).ok()
                } else {
                    None
                };
                out.push(run_info_from_command(&command, state, progress.as_ref()));
            }
        }
    }

    if let Ok(entries) = std::fs::read_dir(base.join("history")) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|value| value.to_str()) != Some("json") {
                continue;
            }
            let Ok(history) = read_run_history(&path) else {
                continue;
            };
            let mut info = run_info_from_command(
                &history.command,
                HostBackupRunState::Finished,
                history.progress.as_ref(),
            );
            info.finished_at = Some(history.finished_at);
            info.success = Some(history.result.success);
            info.error = history.result.error.clone();
            info.files = history.result.files.unwrap_or(info.files);
            info.directories = history.result.directories.unwrap_or(0);
            info.logical_bytes = history.result.logical_bytes.unwrap_or(0);
            info.duration_ms = history.result.duration_ms.unwrap_or(0);
            out.push(info);
        }
    }

    out.sort_by(|a, b| b.backup_time.cmp(&a.backup_time));
    out.truncate(200);
    Ok(out)
}

/// Remove queued/running state when the host itself is deleted. Historical records are deleted
/// with the host because they are only orchestration metadata; actual backups remain in datastores.
pub fn remove_host_runs(host: &str) -> Result<(), Error> {
    let path = run_host_dir(host);
    match std::fs::remove_dir_all(path) {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(err) => Err(err.into()),
    }
}

/// Decide whether a persisted status still represents a connected agent.
pub fn status_is_online(status: &HostAgentStatus, now: i64) -> bool {
    now.saturating_sub(status.last_seen) <= HOST_ONLINE_THRESHOLD
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
struct HostRestoreRunProgressRecord {
    progress: HostRestoreRunProgress,
    started_at: i64,
    updated_at: i64,
}

#[derive(Clone, Debug, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "kebab-case")]
struct HostRestoreRunHistory {
    command: HostRestoreRunCommand,
    result: HostRestoreRunResult,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    progress: Option<HostRestoreRunProgressRecord>,
    finished_at: i64,
}

fn read_restore_command(path: &Path) -> Result<HostRestoreRunCommand, Error> {
    let raw = std::fs::read(path)?;
    serde_json::from_slice(&raw).map_err(|err| {
        format_err!(
            "unable to parse PBS-X host restore run '{}': {err}",
            path.display()
        )
    })
}

fn restore_progress_path(host: &str, run_id: &str) -> PathBuf {
    run_host_dir(host)
        .join("restore-progress")
        .join(format!("{run_id}.json"))
}

fn read_restore_progress(path: &Path) -> Result<HostRestoreRunProgressRecord, Error> {
    let raw = std::fs::read(path)?;
    serde_json::from_slice(&raw).map_err(|err| {
        format_err!(
            "unable to parse PBS-X host restore progress '{}': {err}",
            path.display()
        )
    })
}

fn ensure_initial_restore_progress(command: &HostRestoreRunCommand) -> Result<(), Error> {
    let path = restore_progress_path(&command.host_id, &command.run_id);
    if path.exists() {
        return Ok(());
    }
    let now = proxmox_time::epoch_i64();
    write_run_json(
        &path,
        &HostRestoreRunProgressRecord {
            progress: HostRestoreRunProgress {
                host_id: command.host_id.clone(),
                run_id: command.run_id.clone(),
                stage: Some("starting".to_string()),
                processed_bytes: 0,
                files: 0,
            },
            started_at: now,
            updated_at: now,
        },
    )
}

/// Persist progress for a currently claimed native restore.
pub fn update_host_restore_progress(progress: &HostRestoreRunProgress) -> Result<(), Error> {
    create_run_dirs(&progress.host_id)?;
    let _lock = host_run_lock(&progress.host_id)?;
    let running = run_host_dir(&progress.host_id)
        .join("restore-running")
        .join(format!("{}.json", progress.run_id));
    let command = read_restore_command(&running)
        .map_err(|_| format_err!("restore run '{}' is not currently claimed", progress.run_id))?;
    if command.host_id != progress.host_id || command.run_id != progress.run_id {
        bail!("host restore progress identity mismatch");
    }

    let path = restore_progress_path(&progress.host_id, &progress.run_id);
    let now = proxmox_time::epoch_i64();
    let started_at = read_restore_progress(&path)
        .map(|record| record.started_at)
        .unwrap_or(now);
    write_run_json(
        &path,
        &HostRestoreRunProgressRecord {
            progress: progress.clone(),
            started_at,
            updated_at: now,
        },
    )
}

/// Queue a native restore from a PBS host backup group to an enrolled destination.
pub fn queue_host_restore(
    host: &str,
    backup_id: &str,
    store: &str,
    ns: Option<pbs_api_types::BackupNamespace>,
    backup_time: i64,
    sources: Vec<String>,
    destination: String,
    mode: HostRestoreMode,
) -> Result<HostRestoreRunCommand, Error> {
    let (host_config, _) = pbs_config::host::config()?;
    let host_cfg: HostConfig = host_config
        .lookup("host", host)
        .map_err(|_| format_err!("PBS-X host '{host}' does not exist"))?;
    if !host_cfg.enable {
        bail!("PBS-X host '{host}' is disabled");
    }
    if destination.trim().is_empty() {
        bail!("restore destination must not be empty");
    }
    // The destination's selected storage credential is checked by the ordinary
    // PBS reader. Backup-job targets do not define restore authorization.
    let sources = if sources.is_empty() {
        vec!["/".to_string()]
    } else {
        sources
    };

    create_run_dirs(host)?;
    let _lock = host_run_lock(host)?;
    let requested_at = proxmox_time::epoch_i64();
    let mut random = [0u8; 12];
    proxmox_sys::linux::fill_with_random_data(&mut random)?;
    let run_id = format!("restore-{requested_at}-{}", hex::encode(random));
    let command = HostRestoreRunCommand {
        run_id: run_id.clone(),
        host_id: host.to_string(),
        backup_id: Some(backup_id.to_string()),
        store: store.to_string(),
        ns,
        backup_time,
        sources,
        destination,
        mode,
        requested_at,
    };
    let path = run_host_dir(host)
        .join("restore-queued")
        .join(format!("{run_id}.json"));
    write_run_json(&path, &command)?;
    Ok(command)
}

/// Claim the next restore. Running work is returned again after an agent restart.
pub fn claim_host_restore(host: &str) -> Result<Option<HostRestoreRunCommand>, Error> {
    create_run_dirs(host)?;
    let _lock = host_run_lock(host)?;
    let base = run_host_dir(host);
    if let Some(path) = first_json_file(&base.join("restore-running"))? {
        let command = read_restore_command(&path)?;
        ensure_initial_restore_progress(&command)?;
        return Ok(Some(command));
    }
    let Some(queued) = first_json_file(&base.join("restore-queued"))? else {
        return Ok(None);
    };
    let file_name = queued
        .file_name()
        .ok_or_else(|| format_err!("invalid queued host restore path"))?;
    let running = base.join("restore-running").join(file_name);
    std::fs::rename(&queued, &running)?;
    let command = read_restore_command(&running)?;
    ensure_initial_restore_progress(&command)?;
    Ok(Some(command))
}

/// Finish a claimed restore and retain a bounded orchestration history.
pub fn finish_host_restore(result: &HostRestoreRunResult) -> Result<(), Error> {
    create_run_dirs(&result.host_id)?;
    let _lock = host_run_lock(&result.host_id)?;
    let base = run_host_dir(&result.host_id);
    let running = base
        .join("restore-running")
        .join(format!("{}.json", result.run_id));
    let command = read_restore_command(&running)
        .map_err(|_| format_err!("restore run '{}' is not currently claimed", result.run_id))?;
    if command.host_id != result.host_id || command.run_id != result.run_id {
        bail!("host restore completion identity mismatch");
    }
    let progress_path = restore_progress_path(&result.host_id, &result.run_id);
    let progress = read_restore_progress(&progress_path).ok();
    let history = HostRestoreRunHistory {
        command,
        result: result.clone(),
        progress,
        finished_at: proxmox_time::epoch_i64(),
    };
    write_run_json(
        &base
            .join("restore-history")
            .join(format!("{}.json", result.run_id)),
        &history,
    )?;
    std::fs::remove_file(running)?;
    let _ = std::fs::remove_file(progress_path);

    let history_dir = base.join("restore-history");
    if let Ok(entries) = std::fs::read_dir(&history_dir) {
        let mut files = entries
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .filter(|path| path.extension().and_then(|v| v.to_str()) == Some("json"))
            .collect::<Vec<_>>();
        files.sort();
        if files.len() > 200 {
            let remove_count = files.len() - 200;
            for path in files.into_iter().take(remove_count) {
                let _ = std::fs::remove_file(path);
            }
        }
    }
    Ok(())
}

/// List queued, running and recent native restore orchestration state for one host.
pub fn list_host_restore_runs(host: &str) -> Result<Vec<HostRestoreRunInfo>, Error> {
    create_run_dirs(host)?;
    let _lock = host_run_lock(host)?;
    let base = run_host_dir(host);
    let mut out = Vec::new();

    let make_info = |command: HostRestoreRunCommand,
                     state: HostRestoreRunState,
                     progress: Option<&HostRestoreRunProgressRecord>| {
        let mut info = HostRestoreRunInfo {
            run_id: command.run_id.clone(),
            host_id: command.host_id.clone(),
            backup_id: command.backup_id.clone(),
            state,
            store: command.store.clone(),
            ns: command.ns.clone(),
            backup_time: command.backup_time,
            sources: command.sources.clone(),
            destination: command.destination.clone(),
            mode: command.mode,
            requested_at: command.requested_at,
            started_at: None,
            updated_at: None,
            finished_at: None,
            success: None,
            error: None,
            stage: None,
            processed_bytes: 0,
            files: 0,
            duration_ms: 0,
        };
        if let Some(progress) = progress {
            info.started_at = Some(progress.started_at);
            info.updated_at = Some(progress.updated_at);
            info.stage = progress.progress.stage.clone();
            info.processed_bytes = progress.progress.processed_bytes;
            info.files = progress.progress.files;
        }
        info
    };

    for (dir_name, state) in [
        ("restore-queued", HostRestoreRunState::Queued),
        ("restore-running", HostRestoreRunState::Running),
    ] {
        if let Ok(entries) = std::fs::read_dir(base.join(dir_name)) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.extension().and_then(|v| v.to_str()) != Some("json") {
                    continue;
                }
                let command = read_restore_command(&path)?;
                let progress = if state == HostRestoreRunState::Running {
                    read_restore_progress(&restore_progress_path(host, &command.run_id)).ok()
                } else {
                    None
                };
                out.push(make_info(command, state, progress.as_ref()));
            }
        }
    }

    if let Ok(entries) = std::fs::read_dir(base.join("restore-history")) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path.extension().and_then(|v| v.to_str()) != Some("json") {
                continue;
            }
            let raw = std::fs::read(&path)?;
            let history: HostRestoreRunHistory = serde_json::from_slice(&raw).map_err(|err| {
                format_err!(
                    "unable to parse PBS-X restore history '{}': {err}",
                    path.display()
                )
            })?;
            let mut info = make_info(
                history.command,
                HostRestoreRunState::Finished,
                history.progress.as_ref(),
            );
            info.finished_at = Some(history.finished_at);
            info.success = Some(history.result.success);
            info.error = history.result.error;
            if let Some(started_at) = info.started_at {
                info.duration_ms = history.finished_at.saturating_sub(started_at) as u64 * 1000;
            }
            out.push(info);
        }
    }

    out.sort_by(|a, b| b.requested_at.cmp(&a.requested_at));
    out.truncate(200);
    Ok(out)
}
