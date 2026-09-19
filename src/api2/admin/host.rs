use anyhow::Error;
use serde_json::Value;

use proxmox_router::{http_bail, ApiMethod, Permission, Router, RpcEnvironment, SubdirMap};
use proxmox_schema::api;

use pbs_api_types::{Authid, BackupNamespace, BackupType, JOB_ID_SCHEMA};
use pbsx_api_types::{
    HostAgentHeartbeat, HostAgentStatus, HostBackupJobConfig, HostBackupRunCommand, HostConfig,
    HostListItem, HostSnapshotListItem, HOST_ID_SCHEMA, PRIV_HOST_AGENT, PRIV_HOST_AUDIT,
    PRIV_HOST_MODIFY,
};
use pbs_config::CachedUserInfo;

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
            .map(|status| {
                crate::server::host::status_is_online(status, proxmox_time::epoch_i64())
            })
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
/// List ordinary PBS snapshots for this host across the datastore/namespace targets used by its jobs.
pub async fn list_host_snapshots(
    name: String,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostSnapshotListItem>, Error> {
    if !can_audit_host(&name, rpcenv)? {
        http_bail!(FORBIDDEN, "missing Host.Audit permission for host '{}'", name);
    }

    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let (job_config, _digest) = pbs_config::host_backup::config()?;
    let jobs: Vec<HostBackupJobConfig> = job_config.convert_to_typed_array("host-backup")?;

    let mut targets = Vec::new();
    for job in jobs.into_iter().filter(|job| job.host == name) {
        let ns_key = job.ns.as_ref().map(|ns| ns.to_string()).unwrap_or_default();
        if targets.iter().any(|(store, ns, _): &(String, String, Option<BackupNamespace>)| {
            store == &job.store && ns == &ns_key
        }) {
            continue;
        }
        targets.push((job.store, ns_key, job.ns));
    }

    let host_id = name.clone();
    tokio::task::spawn_blocking(move || {
        let mut result = Vec::new();

        for (store, _ns_key, ns) in targets {
            let snapshots = unsafe {
                crate::api2::admin::datastore::list_snapshots_blocking(
                    store.clone(),
                    ns.clone(),
                    Some(BackupType::Host),
                    Some(host_id.clone()),
                    auth_id.clone(),
                )
            }?;

            for snapshot in snapshots {
                result.push(HostSnapshotListItem {
                    store: store.clone(),
                    ns: ns.clone(),
                    backup_time: snapshot.backup.time,
                    comment: snapshot.comment,
                    size: snapshot.size,
                    protected: snapshot.protected,
                });
            }
        }

        result.sort_by(|a, b| b.backup_time.cmp(&a.backup_time));
        Ok::<_, Error>(result)
    })
    .await
    .map_err(|err| anyhow::format_err!("failed to await host snapshot listing: {err}"))?
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
        http_bail!(BAD_REQUEST, "job '{}' belongs to host '{}', not '{}'", id, job.host, name);
    }
    if !job.enable {
        http_bail!(BAD_REQUEST, "host backup job '{}' is disabled", id);
    }
    crate::server::host::queue_host_backup(&job)
}

const HEARTBEAT_ROUTER: Router = Router::new().post(&API_METHOD_HEARTBEAT);
const BACKUP_ROUTER: Router = Router::new().post(&API_METHOD_RUN_BACKUP);
const SNAPSHOTS_ROUTER: Router = Router::new().get(&API_METHOD_LIST_HOST_SNAPSHOTS);

const ITEM_SUBDIRS: SubdirMap = &[
    ("backup", &BACKUP_ROUTER),
    ("heartbeat", &HEARTBEAT_ROUTER),
    ("snapshots", &SNAPSHOTS_ROUTER),
];

const ITEM_ROUTER: Router = Router::new()
    .get(&API_METHOD_READ_HOST)
    .subdirs(ITEM_SUBDIRS);

pub const ROUTER: Router = Router::new()
    .get(&API_METHOD_LIST_HOSTS)
    .match_all("name", &ITEM_ROUTER);
