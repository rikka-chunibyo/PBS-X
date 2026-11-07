use anyhow::Error;
use serde_json::Value;

use proxmox_router::{ApiMethod, Permission, Router, RpcEnvironment, SubdirMap, http_bail};
use proxmox_schema::api;

use pbs_api_types::{
    Authid, BACKUP_ID_SCHEMA, BACKUP_NAMESPACE_SCHEMA, BACKUP_TIME_SCHEMA, BackupNamespace,
    BackupType, DATASTORE_SCHEMA, JOB_ID_SCHEMA, MAX_NAMESPACE_DEPTH, Operation,
    PRIV_DATASTORE_AUDIT, PRIV_DATASTORE_BACKUP, PRIV_DATASTORE_READ,
};
use pbs_config::CachedUserInfo;
use pbsx_api_types::{
    HOST_ID_SCHEMA, HostAgentHeartbeat, HostAgentStatus, HostBackupJobConfig, HostBackupRunCommand,
    HostBackupRunInfo, HostConfig, HostListItem, HostRestoreMode, HostRestoreRunCommand,
    HostRestoreRunInfo, HostSnapshotListItem, PRIV_HOST_AGENT, PRIV_HOST_AUDIT, PRIV_HOST_MODIFY,
};

fn get_host_config(name: &str) -> Result<HostConfig, Error> {
    let (config, _digest) = pbs_config::host::config()?;
    match config.lookup("host", name) {
        Ok(host) => Ok(host),
        Err(_) => http_bail!(NOT_FOUND, "host '{}' does not exist", name),
    }
}

fn can_read_host(name: &str, rpcenv: &mut dyn RpcEnvironment) -> Result<bool, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let privs = user_info.lookup_privs(&auth_id, &["host", name]);
    Ok(privs & (PRIV_HOST_AUDIT | PRIV_HOST_AGENT) != 0)
}

fn can_audit_host(name: &str, rpcenv: &mut dyn RpcEnvironment) -> Result<bool, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let privs = user_info.lookup_privs(&auth_id, &["host", name]);
    Ok(privs & PRIV_HOST_AUDIT != 0)
}

#[api(
    input: {
        properties: {},
    },
    returns: {
        description: "PBS-X hosts with their latest agent status.",
        type: Array,
        items: { type: HostListItem },
    },
    access: {
        description: "Returns only hosts visible through Host.Audit.",
        permission: &Permission::Anybody,
    },
)]
/// List configured hosts together with their latest heartbeat.
pub fn list_hosts(
    _param: Value,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostListItem>, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let (config, _digest) = pbs_config::host::config()?;
    let hosts: Vec<HostConfig> = config.convert_to_typed_array("host")?;
    let now = proxmox_time::epoch_i64();

    let mut result = Vec::new();
    for host in hosts {
        let privs = user_info.lookup_privs(&auth_id, &["host", &host.name]);
        if privs & PRIV_HOST_AUDIT == 0 {
            continue;
        }

        let status = crate::server::host::read_host_status(&host.name)?;
        let online = host.enable
            && status
                .as_ref()
                .map(|status| crate::server::host::status_is_online(status, now))
                .unwrap_or(false);

        result.push(HostListItem {
            config: host,
            online,
            status,
        });
    }

    Ok(result)
}

#[api(
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
        },
    },
    returns: { type: HostListItem },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Read a host and its current agent status.
pub fn read_host(
    name: String,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<HostListItem, Error> {
    if !can_read_host(&name, rpcenv)? {
        http_bail!(FORBIDDEN, "missing permission to read host '{}'", name);
    }

    let config = get_host_config(&name)?;
    let status = crate::server::host::read_host_status(&name)?;
    let online = config.enable
        && status
            .as_ref()
            .map(|status| crate::server::host::status_is_online(status, proxmox_time::epoch_i64()))
            .unwrap_or(false);

    Ok(HostListItem {
        config,
        online,
        status,
    })
}

#[api(
    protected: true,
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
            heartbeat: {
                type: HostAgentHeartbeat,
                flatten: true,
            },
        },
    },
    returns: { type: HostAgentStatus },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_AGENT, false),
    },
)]
/// Record a heartbeat from a PBS-X agent.
pub fn heartbeat(name: String, heartbeat: HostAgentHeartbeat) -> Result<HostAgentStatus, Error> {
    let host = get_host_config(&name)?;
    if !host.enable {
        http_bail!(FORBIDDEN, "host '{}' is disabled", name);
    }

    let status = HostAgentStatus {
        last_seen: proxmox_time::epoch_i64(),
        heartbeat,
    };
    crate::server::host::write_host_status(&name, &status)?;
    Ok(status)
}

#[api(
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
        },
    },
    returns: {
        description: "Normal PBS host snapshots associated with this registered PBS-X host.",
        type: Array,
        items: { type: HostSnapshotListItem },
    },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// List accessible host histories by device namespace, generated owner or legacy backup ID.
pub async fn list_host_snapshots(
    name: String,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostSnapshotListItem>, Error> {
    if !can_audit_host(&name, rpcenv)? {
        http_bail!(
            FORBIDDEN,
            "missing Host.Audit permission for host '{}'",
            name
        );
    }

    get_host_config(&name)?;
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let stores = crate::api2::admin::datastore::get_datastore_list(Value::Null, _info, rpcenv)?;
    let credentials = crate::server::host::read_host_credentials(&name)?;
    let device_ns = host_device_namespace(&name)?;

    tokio::task::spawn_blocking(move || {
        let mut result = Vec::new();
        for item in stores {
            // An unmounted removable datastore has no currently available snapshots.
            if item.mount_status == pbs_api_types::DataStoreMountStatus::NotMounted {
                continue;
            }
            let store = item.store;
            let datastore = pbs_datastore::DataStore::lookup_datastore(crate::tools::lookup_with(
                &store,
                Operation::Read,
            ))?;
            // Keep the same namespace/owner filtering as normal PBS snapshot listing.
            // Host.Audit establishes association, never datastore authorization.
            let groups = crate::backup::ListAccessibleBackupGroups::new_with_privs(
                &datastore,
                BackupNamespace::root(),
                MAX_NAMESPACE_DEPTH,
                Some(PRIV_DATASTORE_AUDIT),
                Some(PRIV_DATASTORE_BACKUP),
                Some(&auth_id),
            )?;
            for group in groups {
                let group = group?;
                if group.backup_type() != BackupType::Host {
                    continue;
                }
                let owner = group.get_owner()?;
                let ns = group.backup_ns();
                if !host_backup_matches(
                    &name,
                    &device_ns,
                    ns,
                    group.group().id.as_str(),
                    &owner,
                    credentials.as_ref(),
                ) {
                    continue;
                }
                for info in group.list_backups()? {
                    let snapshot = crate::tools::backup_info_to_snapshot_list_item(&info, &owner);
                    result.push(HostSnapshotListItem {
                        store: store.clone(),
                        ns: if ns.is_root() { None } else { Some(ns.clone()) },
                        backup_id: Some(snapshot.backup.group.id),
                        backup_time: snapshot.backup.time,
                        comment: snapshot.comment,
                        size: snapshot.size,
                        protected: snapshot.protected,
                    });
                }
            }
        }
        result.sort_by(|a, b| {
            b.backup_time
                .cmp(&a.backup_time)
                .then_with(|| a.store.cmp(&b.store))
                .then_with(|| {
                    a.ns.as_ref()
                        .map(ToString::to_string)
                        .cmp(&b.ns.as_ref().map(ToString::to_string))
                })
                .then_with(|| a.backup_id.cmp(&b.backup_id))
        });
        Ok::<_, Error>(result)
    })
    .await
    .map_err(|err| anyhow::format_err!("failed to await host snapshot listing: {err}"))?
}

fn host_device_namespace(host: &str) -> Result<BackupNamespace, Error> {
    // Host IDs are ASCII safe IDs; use the same 32-character components as Android/the job form.
    let parts: Vec<&str> = host
        .as_bytes()
        .chunks(32)
        .map(|part| std::str::from_utf8(part))
        .collect::<Result<_, _>>()?;
    format!("devices/{}", parts.join("/")).parse()
}

fn host_backup_matches(
    host: &str,
    device_ns: &BackupNamespace,
    ns: &BackupNamespace,
    backup_id: &str,
    owner: &Authid,
    credentials: Option<&crate::server::host::HostAgentCredentialRecord>,
) -> bool {
    backup_id == host
        || device_ns.contains(ns).is_some()
        || credentials.is_some_and(|record| record.known_owner(&owner.to_string()))
}

#[api(
    input: {
        properties: {
            name: { schema: HOST_ID_SCHEMA },
        },
    },
    returns: {
        description: "Current and recent PBS-X host backup runs.",
        type: Array,
        items: { type: HostBackupRunInfo },
    },
    access: { permission: &Permission::Anybody },
)]
/// List orchestration state for this host. Datastore snapshots remain the source of truth for backups.
pub fn list_backup_runs(
    name: String,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostBackupRunInfo>, Error> {
    if !can_audit_host(&name, rpcenv)? {
        http_bail!(
            FORBIDDEN,
            "missing Host.Audit permission for host '{}'",
            name
        );
    }
    crate::server::host::list_host_backup_runs(&name)
}

#[api(
    input: {
        properties: {
            name: { schema: HOST_ID_SCHEMA },
        },
    },
    returns: {
        description: "Current and recent PBS-X native host restores.",
        type: Array,
        items: { type: HostRestoreRunInfo },
    },
    access: { permission: &Permission::Anybody },
)]
/// List restore orchestration state for this host.
pub fn list_restore_runs(
    name: String,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostRestoreRunInfo>, Error> {
    if !can_audit_host(&name, rpcenv)? {
        http_bail!(
            FORBIDDEN,
            "missing Host.Audit permission for host '{}'",
            name
        );
    }
    crate::server::host::list_host_restore_runs(&name)
}

#[api(
    protected: true,
    input: {
        properties: {
            name: { schema: HOST_ID_SCHEMA },
            id: { schema: JOB_ID_SCHEMA },
        },
    },
    returns: { type: HostBackupRunCommand },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_MODIFY, false),
    },
)]
/// Queue a configured host backup job for immediate pickup by its agent.
pub fn run_backup(name: String, id: String) -> Result<HostBackupRunCommand, Error> {
    let host = get_host_config(&name)?;
    if !host.enable {
        http_bail!(FORBIDDEN, "host '{}' is disabled", name);
    }
    let (config, _) = pbs_config::host_backup::config()?;
    let job: HostBackupJobConfig = config
        .lookup("host-backup", &id)
        .map_err(|_| anyhow::format_err!("host backup job '{}' does not exist", id))?;
    if job.host != name {
        http_bail!(
            BAD_REQUEST,
            "job '{}' belongs to host '{}', not '{}'",
            id,
            job.host,
            name
        );
    }
    if !job.enable {
        http_bail!(BAD_REQUEST, "host backup job '{}' is disabled", id);
    }
    crate::server::host::queue_host_backup(&job)
}

#[api(
    protected: true,
    input: {
        properties: {
            name: { schema: HOST_ID_SCHEMA },
            store: { schema: DATASTORE_SCHEMA },
            ns: { optional: true, schema: BACKUP_NAMESPACE_SCHEMA },
            "backup-id": { optional: true, schema: BACKUP_ID_SCHEMA },
            "backup-time": { schema: BACKUP_TIME_SCHEMA },
            sources: {
                optional: true,
                type: Array,
                description: "Archive paths selected for this restore.",
                items: {
                    type: String,
                    description: "Archive path selected for restore.",
                },
            },
            destination: {
                type: String,
                description: "Destination directory on the protected host.",
            },
            mode: { type: HostRestoreMode, optional: true },
            "target-host": { optional: true, schema: HOST_ID_SCHEMA },
        },
    },
    returns: { type: HostRestoreRunCommand },
    access: {
        permission: &Permission::Anybody,
    },
)]
/// Restore this host's backup to a selected agent using that agent's storage credential.
#[allow(clippy::too_many_arguments)]
pub fn run_restore(
    name: String,
    store: String,
    ns: Option<BackupNamespace>,
    backup_id: Option<String>,
    backup_time: i64,
    sources: Option<Vec<String>>,
    destination: String,
    mode: Option<HostRestoreMode>,
    target_host: Option<String>,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<HostRestoreRunCommand, Error> {
    let target_host = target_host.unwrap_or_else(|| name.clone());
    let host = get_host_config(&target_host)?;
    if !host.enable {
        http_bail!(FORBIDDEN, "host '{}' is disabled", target_host);
    }

    // Restoring is a data-read operation. Host.Modify alone must not allow an operator to
    // exfiltrate backup contents onto a machine they control without datastore read rights.
    let auth_id: Authid = rpcenv
        .get_auth_id()
        .ok_or_else(|| anyhow::format_err!("missing authentication ID"))?
        .parse()?;
    let user_info = CachedUserInfo::new()?;
    user_info.check_privs(&auth_id, &["host", &target_host], PRIV_HOST_MODIFY, false)?;
    let effective_ns = ns.clone().unwrap_or_else(BackupNamespace::root);
    if let Err(err) = user_info.check_privs(
        &auth_id,
        &effective_ns.acl_path(&store),
        PRIV_DATASTORE_READ,
        false,
    ) {
        http_bail!(FORBIDDEN, "{err}");
    }

    crate::server::host::queue_host_restore(
        &target_host,
        backup_id.as_deref().unwrap_or(&name),
        &store,
        ns,
        backup_time,
        sources.unwrap_or_else(|| vec!["/".to_string()]),
        destination,
        mode.unwrap_or_default(),
    )
}

const HEARTBEAT_ROUTER: Router = Router::new().post(&API_METHOD_HEARTBEAT);
const BACKUP_ROUTER: Router = Router::new().post(&API_METHOD_RUN_BACKUP);
const RUNS_ROUTER: Router = Router::new().get(&API_METHOD_LIST_BACKUP_RUNS);
const RESTORE_RUNS_ROUTER: Router = Router::new().get(&API_METHOD_LIST_RESTORE_RUNS);
const SNAPSHOTS_ROUTER: Router = Router::new().get(&API_METHOD_LIST_HOST_SNAPSHOTS);
const RESTORE_ROUTER: Router = Router::new().post(&API_METHOD_RUN_RESTORE);

const ITEM_SUBDIRS: SubdirMap = &[
    ("backup", &BACKUP_ROUTER),
    ("heartbeat", &HEARTBEAT_ROUTER),
    ("restore", &RESTORE_ROUTER),
    ("restore-runs", &RESTORE_RUNS_ROUTER),
    ("runs", &RUNS_ROUTER),
    ("snapshots", &SNAPSHOTS_ROUTER),
];

const ITEM_ROUTER: Router = Router::new()
    .get(&API_METHOD_READ_HOST)
    .subdirs(ITEM_SUBDIRS);

pub const ROUTER: Router = Router::new()
    .get(&API_METHOD_LIST_HOSTS)
    .match_all("name", &ITEM_ROUTER);

#[cfg(test)]
mod host_history_tests {
    use super::*;

    #[test]
    fn histories_follow_namespace_generated_owners_and_legacy_ids() {
        let host = "test-phone";
        let device_ns = host_device_namespace(host).unwrap();
        let root = BackupNamespace::root();
        let other: BackupNamespace = "shared/elsewhere".parse().unwrap();
        let owner: Authid = "root@pam!manual".parse().unwrap();
        let credentials: crate::server::host::HostAgentCredentialRecord =
            serde_json::from_value(serde_json::json!({
                "certificate-fingerprint": "fixture",
                "backup-auth-id": "root@pam!current",
                "previous-backup-auth-ids": ["root@pam!retained"],
                "retired-backup-auth-ids": ["root@pam!revoked"],
                "issued": 1,
            }))
            .unwrap();
        let matches = |ns: &BackupNamespace, id: &str, owner: &Authid| {
            host_backup_matches(host, &device_ns, ns, id, owner, Some(&credentials))
        };
        assert!(matches(&root, host, &owner));
        assert!(matches(&device_ns, "photos", &owner));
        assert!(matches(
            &"devices/test-phone/old".parse().unwrap(),
            "photos",
            &owner
        ));
        assert!(!matches(
            &"devices/test-phone-other".parse().unwrap(),
            "photos",
            &owner
        ));
        assert!(!matches(&root, "photos", &owner));
        for key in ["current", "retained", "revoked"] {
            assert!(matches(
                &other,
                "custom-id",
                &format!("root@pam!{key}").parse().unwrap()
            ));
        }
        assert!(!matches(&other, "custom-id", &owner));
        assert!(host_backup_matches(
            host, &device_ns, &device_ns, "photos", &owner, None
        ));
    }

    #[test]
    fn long_device_namespace_matches_complete_components() {
        let host = "a".repeat(64);
        let device_ns = host_device_namespace(&host).unwrap();
        assert_eq!(
            device_ns.to_string(),
            format!("devices/{}/{}", "a".repeat(32), "a".repeat(32))
        );
        assert!(
            device_ns
                .contains(&format!("{device_ns}/photos").parse().unwrap())
                .is_some()
        );
        assert!(
            device_ns
                .contains(&format!("devices/{}", "a".repeat(32)).parse().unwrap())
                .is_none()
        );
    }
}
