use anyhow::{Error, bail};
use serde::{Deserialize, Serialize};

use proxmox_router::{Permission, Router, RpcEnvironment};
use proxmox_schema::{api, param_bail};

use pbs_api_types::{
    Authid, BackupNamespace, JOB_ID_SCHEMA, Operation, PRIV_DATASTORE_AUDIT, PRIV_DATASTORE_BACKUP,
    PROXMOX_CONFIG_DIGEST_SCHEMA,
};
use pbs_config::CachedUserInfo;
use pbsx_api_types::{
    HostBackupJobConfig, HostBackupJobConfigUpdater, PRIV_HOST_AUDIT, PRIV_HOST_MODIFY,
};

fn host_exists(host: &str) -> Result<bool, Error> {
    let (config, _digest) = pbs_config::host::config()?;
    Ok(config.sections.contains_key(host))
}

// Creating an approved job may provision its namespace, but only with the same
// Datastore.Modify checks as the ordinary namespace API. Device keys receive
// DatastoreBackup, never namespace creation privileges.
fn ensure_job_namespace(
    job: &HostBackupJobConfig,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    let Some(ns) = &job.ns else {
        return Ok(());
    };
    if ns.is_root() {
        return Ok(());
    }
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let datastore = pbs_datastore::DataStore::lookup_datastore(crate::tools::lookup_with(
        &job.store,
        Operation::Write,
    ))?;
    let mut parent = BackupNamespace::root();
    let mut missing = Vec::new();
    for part in ns.components() {
        let child = BackupNamespace::from_parent_ns(&parent, part.to_string())?;
        if !datastore.namespace_exists(&child) {
            crate::backup::check_ns_modification_privs(&job.store, &child, &auth_id)?;
            missing.push((parent, part.to_string()));
        }
        parent = child;
    }
    let backup_user = pbs_config::backup_user()?;
    let options = proxmox_sys::fs::CreateOptions::new()
        .owner(backup_user.uid)
        .group(backup_user.gid)
        .perm(nix::sys::stat::Mode::from_bits_truncate(0o0750));
    for (parent, part) in missing {
        // This protected API runs as root, whereas native backup uploads run
        // as backup. Create only missing directories with backup ownership,
        // including each namespace's intermediate `ns/` container.
        let child = BackupNamespace::from_parent_ns(&parent, part.clone())?;
        proxmox_sys::fs::create_path(
            datastore.namespace_path(&child),
            Some(options),
            Some(options),
        )?;
        datastore.create_namespace(&parent, part)?;
    }
    Ok(())
}

fn check_read_access(
    user_info: &CachedUserInfo,
    auth_id: &Authid,
    job: &HostBackupJobConfig,
) -> bool {
    let host_privs = user_info.lookup_privs(auth_id, &["host", &job.host]);
    if host_privs & PRIV_HOST_AUDIT == 0 {
        return false;
    }

    let datastore_privs = user_info.lookup_privs(auth_id, &job.datastore_acl_path());
    datastore_privs & PRIV_DATASTORE_AUDIT != 0
}

fn check_modify_access(
    user_info: &CachedUserInfo,
    auth_id: &Authid,
    job: &HostBackupJobConfig,
) -> bool {
    let host_privs = user_info.lookup_privs(auth_id, &["host", &job.host]);
    if host_privs & PRIV_HOST_MODIFY == 0 {
        return false;
    }

    let datastore_privs = user_info.lookup_privs(auth_id, &job.datastore_acl_path());
    datastore_privs & PRIV_DATASTORE_BACKUP != 0
}

#[api(
    input: {
        properties: {
            host: {
                optional: true,
                schema: pbsx_api_types::HOST_ID_SCHEMA,
            },
        },
    },
    returns: {
        description: "Native PBS-X host backup jobs visible to the caller.",
        type: Array,
        items: { type: HostBackupJobConfig },
    },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// List configured host backup jobs.
pub fn list_host_backup_jobs(
    host: Option<String>,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostBackupJobConfig>, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let (config, digest) = pbs_config::host_backup::config()?;

    let jobs: Vec<HostBackupJobConfig> = config.convert_to_typed_array("host-backup")?;
    let jobs = jobs
        .into_iter()
        .filter(|job| host.as_ref().map(|host| host == &job.host).unwrap_or(true))
        .filter(|job| check_read_access(&user_info, &auth_id, job))
        .collect();

    rpcenv["digest"] = hex::encode(digest).into();
    Ok(jobs)
}

#[api(
    protected: true,
    input: {
        properties: {
            config: {
                type: HostBackupJobConfig,
                flatten: true,
            },
        },
    },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Create a native host backup job.
pub fn create_host_backup_job(
    config: HostBackupJobConfig,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;

    if !host_exists(&config.host)? {
        param_bail!("host", "host '{}' does not exist", config.host);
    }
    if config.sources.is_empty() {
        param_bail!("sources", "at least one source is required",);
    }
    if !check_modify_access(&user_info, &auth_id, &config) {
        bail!("permission check failed");
    }

    let _lock = pbs_config::host_backup::lock_config()?;
    let (mut section_config, _digest) = pbs_config::host_backup::config()?;
    if section_config.sections.contains_key(&config.id) {
        param_bail!("id", "job '{}' already exists", config.id);
    }

    ensure_job_namespace(&config, rpcenv)?;
    crate::server::host::initialize_host_upload_acl_tracking(&config.host)?;
    section_config.set_data(&config.id, "host-backup", &config)?;
    pbs_config::host_backup::save_config(&section_config)?;
    crate::server::jobstate::create_state_file("hostbackup", &config.id)?;
    crate::server::host::sync_host_upload_acl(&config.host)?;
    Ok(())
}

#[api(
    input: {
        properties: {
            id: { schema: JOB_ID_SCHEMA },
        },
    },
    returns: { type: HostBackupJobConfig },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Read one host backup job.
pub fn read_host_backup_job(
    id: String,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<HostBackupJobConfig, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let (config, digest) = pbs_config::host_backup::config()?;
    let job: HostBackupJobConfig = config.lookup("host-backup", &id)?;

    if !check_read_access(&user_info, &auth_id, &job) {
        bail!("permission check failed");
    }

    rpcenv["digest"] = hex::encode(digest).into();
    Ok(job)
}

#[api]
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
/// Optional host backup job properties which may be reset.
pub enum DeletableProperty {
    /// Reset the backup namespace to the root namespace.
    Namespace,
    /// Clear the configured backup schedule.
    Schedule,
    /// Clear the administrator-facing job comment.
    Comment,
}

#[api(
    protected: true,
    input: {
        properties: {
            id: { schema: JOB_ID_SCHEMA },
            update: {
                type: HostBackupJobConfigUpdater,
                flatten: true,
            },
            delete: {
                type: Array,
                optional: true,
                description: "Optional properties to reset to their defaults.",
                items: { type: DeletableProperty },
            },
            digest: {
                optional: true,
                schema: PROXMOX_CONFIG_DIGEST_SCHEMA,
            },
        },
    },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Update a native host backup job.
pub fn update_host_backup_job(
    id: String,
    update: HostBackupJobConfigUpdater,
    delete: Option<Vec<DeletableProperty>>,
    digest: Option<String>,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let _lock = pbs_config::host_backup::lock_config()?;
    let (mut config, expected_digest) = pbs_config::host_backup::config()?;

    pbs_config::detect_modified_configuration_file(digest.as_deref(), &expected_digest)?;

    let mut data: HostBackupJobConfig = config.lookup("host-backup", &id)?;
    let previous_host = data.host.clone();
    if !check_modify_access(&user_info, &auth_id, &data) {
        bail!("permission check failed");
    }

    crate::server::host::initialize_host_upload_acl_tracking(&previous_host)?;

    if let Some(delete) = delete {
        for prop in delete {
            match prop {
                DeletableProperty::Namespace => data.ns = None,
                DeletableProperty::Schedule => data.schedule = None,
                DeletableProperty::Comment => data.comment = None,
            }
        }
    }

    if let Some(host) = update.host {
        if !host_exists(&host)? {
            param_bail!("host", "host '{}' does not exist", host);
        }
        data.host = host;
    }
    if let Some(store) = update.store {
        data.store = store;
    }
    if let Some(ns) = update.ns {
        data.ns = Some(ns);
    }
    if let Some(sources) = update.sources {
        if sources.is_empty() {
            param_bail!("sources", "at least one source is required",);
        }
        data.sources = sources;
    }
    if let Some(excludes) = update.excludes {
        data.excludes = excludes;
    }
    if let Some(schedule) = update.schedule {
        data.schedule = Some(schedule);
    }
    if let Some(snapshot_mode) = update.snapshot_mode {
        data.snapshot_mode = snapshot_mode;
    }
    if let Some(change_detection_mode) = update.change_detection_mode {
        data.change_detection_mode = change_detection_mode;
    }
    if let Some(enable) = update.enable {
        data.enable = enable;
    }
    if let Some(comment) = update.comment {
        let comment = comment.trim().to_string();
        data.comment = (!comment.is_empty()).then_some(comment);
    }

    if !check_modify_access(&user_info, &auth_id, &data) {
        bail!("permission check failed for updated target");
    }

    ensure_job_namespace(&data, rpcenv)?;
    if data.host != previous_host {
        crate::server::host::initialize_host_upload_acl_tracking(&data.host)?;
    }
    config.set_data(&id, "host-backup", &data)?;
    pbs_config::host_backup::save_config(&config)?;
    crate::server::host::sync_host_upload_acl(&previous_host)?;
    if data.host != previous_host {
        crate::server::host::sync_host_upload_acl(&data.host)?;
    }
    Ok(())
}

#[api(
    protected: true,
    input: {
        properties: {
            id: { schema: JOB_ID_SCHEMA },
            digest: {
                optional: true,
                schema: PROXMOX_CONFIG_DIGEST_SCHEMA,
            },
        },
    },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Delete a host backup job.
pub fn delete_host_backup_job(
    id: String,
    digest: Option<String>,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let _lock = pbs_config::host_backup::lock_config()?;
    let (mut config, expected_digest) = pbs_config::host_backup::config()?;

    pbs_config::detect_modified_configuration_file(digest.as_deref(), &expected_digest)?;

    let job: HostBackupJobConfig = config.lookup("host-backup", &id)?;
    if !check_modify_access(&user_info, &auth_id, &job) {
        bail!("permission check failed");
    }

    crate::server::host::initialize_host_upload_acl_tracking(&job.host)?;
    config.sections.remove(&id);
    pbs_config::host_backup::save_config(&config)?;
    crate::server::jobstate::remove_state_file("hostbackup", &id)?;
    crate::server::host::sync_host_upload_acl(&job.host)?;
    Ok(())
}

const ITEM_ROUTER: Router = Router::new()
    .get(&API_METHOD_READ_HOST_BACKUP_JOB)
    .put(&API_METHOD_UPDATE_HOST_BACKUP_JOB)
    .delete(&API_METHOD_DELETE_HOST_BACKUP_JOB);

pub const ROUTER: Router = Router::new()
    .get(&API_METHOD_LIST_HOST_BACKUP_JOBS)
    .post(&API_METHOD_CREATE_HOST_BACKUP_JOB)
    .match_all("id", &ITEM_ROUTER);
