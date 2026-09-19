//! PBS-X agent API served directly by the normal PBS proxy on port 8007.
//! Enrollment is authenticated by a short-lived one-time token. After enrollment
//! the generated host-scoped PBS API token authenticates heartbeats, job claims,
//! completion reports and native datastore uploads.

use anyhow::{bail, format_err, Error};
use openssl::nid::Nid;
use openssl::x509::X509;

use proxmox_router::{list_subdirs_api_method, Permission, Router, RpcEnvironment, SubdirMap};
use proxmox_schema::api;

use pbs_api_types::Authid;
use pbs_config::CachedUserInfo;
use pbsx_api_types::{
    HostAgentHeartbeat, HostAgentStatus, HostBackupRunCommand, HostBackupRunProgress,
    HostBackupRunResult, HostRestoreRunCommand, HostRestoreRunProgress, HostRestoreRunResult,
    HostBootstrapRequest, HostBootstrapResponse, HostConfig, HOST_ID_SCHEMA, PRIV_HOST_AGENT,
};

fn ensure_agent_access(host_id: &str, rpcenv: &mut dyn RpcEnvironment) -> Result<(), Error> {
    let auth_id: Authid = rpcenv
        .get_auth_id()
        .ok_or_else(|| format_err!("missing authentication ID"))?
        .parse()?;
    let user_info = CachedUserInfo::new()?;
    let privs = user_info.lookup_privs(&auth_id, &["host", host_id]);
    if privs & PRIV_HOST_AGENT == 0 {
        bail!("missing Host.Agent privilege for host '{host_id}'");
    }
    let (config, _) = pbs_config::host::config()?;
    let host: HostConfig = config
        .lookup("host", host_id)
        .map_err(|_| format_err!("unknown PBS-X host '{host_id}'"))?;
    if !host.enable {
        bail!("host '{host_id}' is disabled");
    }
    Ok(())
}

#[api(
    protected: true,
    input: {
        properties: {
            request: {
                type: HostBootstrapRequest,
                flatten: true,
            },
        },
    },
    returns: { type: HostBootstrapResponse },
    access: {
        description: "The one-time host enrollment token is the credential for this endpoint.",
        permission: &Permission::World,
    },
)]
/// Enroll a PBS-X host agent and return its narrowly scoped native PBS upload token.
pub fn bootstrap_agent(request: HostBootstrapRequest) -> Result<HostBootstrapResponse, Error> {
    let (config, _) = pbs_config::host::config()?;
    let host: HostConfig = config
        .lookup("host", &request.host_id)
        .map_err(|_| format_err!("unknown PBS-X host '{}'", request.host_id))?;
    if !host.enable {
        bail!("host '{}' is disabled", request.host_id);
    }

    // The agent creates its own long-lived TLS key so the private key never leaves the machine.
    // Enrollment binds the exact certificate fingerprint to the configured Host ID.
    let cert = X509::from_pem(request.client_certificate.as_bytes())
        .map_err(|err| format_err!("invalid agent client certificate - {err}"))?;
    let common_name = cert
        .subject_name()
        .entries_by_nid(Nid::COMMONNAME)
        .next()
        .ok_or_else(|| format_err!("agent certificate has no common name"))?
        .data()
        .as_utf8()
        .map_err(|err| format_err!("invalid agent certificate common name - {err}"))?
        .to_string();
    if common_name != request.host_id {
        bail!(
            "agent certificate identity '{}' does not match host '{}'",
            common_name,
            request.host_id
        );
    }
    let pubkey = cert.public_key()?;
    if !cert.verify(&pubkey)? {
        bail!("agent enrollment certificate is not self-signed by its submitted key");
    }
    let cert_der = cert.to_der()?;
    let certificate_fingerprint = hex::encode(openssl::sha::sha256(&cert_der));

    crate::server::host::consume_enrollment_token(&request.host_id, &request.token)?;
    let (backup_auth_id, backup_secret) =
        crate::server::host::issue_host_upload_token(&request.host_id)?;

    let record = crate::server::host::HostAgentCredentialRecord {
        certificate_fingerprint,
        backup_auth_id: backup_auth_id.clone(),
        issued: proxmox_time::epoch_i64(),
    };
    if let Err(err) = crate::server::host::write_host_credentials(&request.host_id, &record) {
        let _ = crate::server::host::revoke_host_upload_auth(&backup_auth_id);
        let _ = crate::server::host::remove_host_credentials(&request.host_id);
        return Err(err);
    }

    crate::server::host::write_host_status(
        &request.host_id,
        &HostAgentStatus {
            last_seen: proxmox_time::epoch_i64(),
            heartbeat: request.heartbeat,
        },
    )?;

    Ok(HostBootstrapResponse {
        backup_auth_id,
        backup_secret,
    })
}

#[api(
    protected: true,
    input: {
        properties: {
            "host-id": { schema: HOST_ID_SCHEMA },
            heartbeat: { type: HostAgentHeartbeat, flatten: true },
        },
    },
    returns: { type: HostAgentStatus },
    access: { permission: &Permission::Anybody },
)]
/// Refresh agent inventory/online state using the host-scoped API token.
pub fn update_status(
    host_id: String,
    heartbeat: HostAgentHeartbeat,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<HostAgentStatus, Error> {
    ensure_agent_access(&host_id, rpcenv)?;
    let status = HostAgentStatus {
        last_seen: proxmox_time::epoch_i64(),
        heartbeat,
    };
    crate::server::host::write_host_status(&host_id, &status)?;
    Ok(status)
}

#[api(
    protected: true,
    input: {
        properties: {
            "host-id": { schema: HOST_ID_SCHEMA },
        },
    },
    returns: {
        description: "Zero or one claimed native host backup runs.",
        type: Array,
        items: { type: HostBackupRunCommand },
    },
    access: { permission: &Permission::Anybody },
)]
/// Claim the next queued backup for this host. Existing running work is returned after agent restart.
pub fn claim_backup(
    host_id: String,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostBackupRunCommand>, Error> {
    ensure_agent_access(&host_id, rpcenv)?;
    Ok(crate::server::host::claim_host_backup(&host_id)?
        .into_iter()
        .collect())
}

#[api(
    protected: true,
    input: {
        properties: {
            progress: { type: HostBackupRunProgress, flatten: true },
        },
    },
    access: { permission: &Permission::Anybody },
)]
/// Persist progress for a currently claimed native host backup run.
pub fn update_backup_progress(
    progress: HostBackupRunProgress,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    ensure_agent_access(&progress.host_id, rpcenv)?;
    crate::server::host::update_host_backup_progress(&progress)
}

#[api(
    protected: true,
    input: {
        properties: {
            result: { type: HostBackupRunResult, flatten: true },
        },
    },
    access: { permission: &Permission::Anybody },
)]
/// Finish a claimed native host backup run.
pub fn finish_backup(
    result: HostBackupRunResult,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    ensure_agent_access(&result.host_id, rpcenv)?;
    crate::server::host::finish_host_backup(&result)
}


#[api(
    protected: true,
    input: {
        properties: {
            "host-id": { schema: HOST_ID_SCHEMA },
        },
    },
    returns: {
        description: "Zero or one claimed native host restore runs.",
        type: Array,
        items: { type: HostRestoreRunCommand },
    },
    access: { permission: &Permission::Anybody },
)]
/// Claim the next direct PBS reader restore for this host.
pub fn claim_restore(
    host_id: String,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostRestoreRunCommand>, Error> {
    ensure_agent_access(&host_id, rpcenv)?;
    Ok(crate::server::host::claim_host_restore(&host_id)?
        .into_iter()
        .collect())
}

#[api(
    protected: true,
    input: {
        properties: {
            progress: { type: HostRestoreRunProgress, flatten: true },
        },
    },
    access: { permission: &Permission::Anybody },
)]
/// Persist progress for a currently claimed direct host restore.
pub fn update_restore_progress(
    progress: HostRestoreRunProgress,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    ensure_agent_access(&progress.host_id, rpcenv)?;
    crate::server::host::update_host_restore_progress(&progress)
}

#[api(
    protected: true,
    input: {
        properties: {
            result: { type: HostRestoreRunResult, flatten: true },
        },
    },
    access: { permission: &Permission::Anybody },
)]
/// Finish a claimed direct host restore.
pub fn finish_restore(
    result: HostRestoreRunResult,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<(), Error> {
    ensure_agent_access(&result.host_id, rpcenv)?;
    crate::server::host::finish_host_restore(&result)
}

const BOOTSTRAP_ROUTER: Router = Router::new().post(&API_METHOD_BOOTSTRAP_AGENT);
const STATUS_ROUTER: Router = Router::new().post(&API_METHOD_UPDATE_STATUS);
const NEXT_ROUTER: Router = Router::new().get(&API_METHOD_CLAIM_BACKUP);
const PROGRESS_ROUTER: Router = Router::new().post(&API_METHOD_UPDATE_BACKUP_PROGRESS);
const FINISH_ROUTER: Router = Router::new().post(&API_METHOD_FINISH_BACKUP);
const RESTORE_NEXT_ROUTER: Router = Router::new().get(&API_METHOD_CLAIM_RESTORE);
const RESTORE_PROGRESS_ROUTER: Router = Router::new().post(&API_METHOD_UPDATE_RESTORE_PROGRESS);
const RESTORE_FINISH_ROUTER: Router = Router::new().post(&API_METHOD_FINISH_RESTORE);

const SUBDIRS: SubdirMap = &[
    ("bootstrap", &BOOTSTRAP_ROUTER),
    ("finish", &FINISH_ROUTER),
    ("next", &NEXT_ROUTER),
    ("progress", &PROGRESS_ROUTER),
    ("restore-finish", &RESTORE_FINISH_ROUTER),
    ("restore-next", &RESTORE_NEXT_ROUTER),
    ("restore-progress", &RESTORE_PROGRESS_ROUTER),
    ("status", &STATUS_ROUTER),
];

pub const ROUTER: Router = Router::new()
    .get(&list_subdirs_api_method!(SUBDIRS))
    .subdirs(SUBDIRS);
