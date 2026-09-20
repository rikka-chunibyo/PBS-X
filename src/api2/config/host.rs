use anyhow::{Error, bail};
use serde::{Deserialize, Serialize};
use serde_json::Value;

use proxmox_router::{ApiMethod, Permission, Router, RpcEnvironment};
use proxmox_schema::api;

use pbs_api_types::{Authid, PROXMOX_CONFIG_DIGEST_SCHEMA};
use pbs_config::CachedUserInfo;
use pbsx_api_types::{
    HOST_ID_SCHEMA, HostConfig, HostConfigUpdater, HostEnrollmentToken, PRIV_HOST_AUDIT,
    PRIV_HOST_MODIFY,
};

#[api(
    input: {
        properties: {},
    },
    returns: {
        description: "List of configured PBS-X hosts.",
        type: Array,
        items: { type: HostConfig },
    },
    access: {
        description: "List hosts filtered by Host.Audit privileges.",
        permission: &Permission::Anybody,
    },
)]
/// List configured PBS-X hosts.
pub fn list_hosts(
    _param: Value,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<Vec<HostConfig>, Error> {
    let auth_id: Authid = rpcenv.get_auth_id().unwrap().parse()?;
    let user_info = CachedUserInfo::new()?;
    let (config, digest) = pbs_config::host::config()?;

    let list: Vec<HostConfig> = config.convert_to_typed_array("host")?;
    let list = list
        .into_iter()
        .filter(|host| {
            let privs = user_info.lookup_privs(&auth_id, &["host", &host.name]);
            privs & PRIV_HOST_AUDIT != 0
        })
        .collect();

    rpcenv["digest"] = hex::encode(digest).into();
    Ok(list)
}

#[api(
    protected: true,
    input: {
        properties: {
            config: {
                type: HostConfig,
                flatten: true,
            },
        },
    },
    access: {
        permission: &Permission::Privilege(&["host"], PRIV_HOST_MODIFY, false),
    },
)]
/// Create a PBS-X host definition.
pub fn create_host(config: HostConfig) -> Result<(), Error> {
    let _lock = pbs_config::host::lock_config()?;
    let (mut section_config, _digest) = pbs_config::host::config()?;

    if section_config.sections.contains_key(&config.name) {
        bail!("host '{}' already exists.", config.name);
    }

    section_config.set_data(&config.name, "host", &config)?;
    pbs_config::host::save_config(&section_config)?;
    Ok(())
}

#[api(
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
        },
    },
    returns: { type: HostConfig },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_AUDIT, false),
    },
)]
/// Read a PBS-X host definition.
pub fn read_host(
    name: String,
    _info: &ApiMethod,
    rpcenv: &mut dyn RpcEnvironment,
) -> Result<HostConfig, Error> {
    let (config, digest) = pbs_config::host::config()?;
    let data: HostConfig = config.lookup("host", &name)?;
    rpcenv["digest"] = hex::encode(digest).into();
    Ok(data)
}

#[api]
#[derive(Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
/// Host properties that can be reset to their defaults.
pub enum DeletableProperty {
    /// Reset the enabled state to true.
    Enable,
    /// Delete the comment.
    Comment,
}

#[api(
    protected: true,
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
            update: {
                type: HostConfigUpdater,
                flatten: true,
            },
            delete: {
                description: "List of properties to reset/delete.",
                type: Array,
                optional: true,
                items: {
                    type: DeletableProperty,
                },
            },
            digest: {
                optional: true,
                schema: PROXMOX_CONFIG_DIGEST_SCHEMA,
            },
        },
    },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_MODIFY, false),
    },
)]
/// Update a PBS-X host definition.
pub fn update_host(
    name: String,
    update: HostConfigUpdater,
    delete: Option<Vec<DeletableProperty>>,
    digest: Option<String>,
) -> Result<(), Error> {
    let _lock = pbs_config::host::lock_config()?;
    let (mut config, expected_digest) = pbs_config::host::config()?;

    pbs_config::detect_modified_configuration_file(digest.as_deref(), &expected_digest)?;

    let mut data: HostConfig = config.lookup("host", &name)?;

    if let Some(delete) = delete {
        for delete_prop in delete {
            match delete_prop {
                DeletableProperty::Enable => data.enable = true,
                DeletableProperty::Comment => data.comment = None,
            }
        }
    }

    if let Some(enable) = update.enable {
        data.enable = enable;
    }

    if let Some(comment) = update.comment {
        let comment = comment.trim().to_string();
        data.comment = if comment.is_empty() {
            None
        } else {
            Some(comment)
        };
    }

    config.set_data(&name, "host", &data)?;
    pbs_config::host::save_config(&config)?;
    Ok(())
}

#[api(
    protected: true,
    input: {
        properties: {
            name: {
                schema: HOST_ID_SCHEMA,
            },
            digest: {
                optional: true,
                schema: PROXMOX_CONFIG_DIGEST_SCHEMA,
            },
        },
    },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_MODIFY, false),
    },
)]
/// Delete a PBS-X host definition and its cached runtime status.
pub fn delete_host(name: String, digest: Option<String>) -> Result<(), Error> {
    let _lock = pbs_config::host::lock_config()?;
    let (mut config, expected_digest) = pbs_config::host::config()?;

    pbs_config::detect_modified_configuration_file(digest.as_deref(), &expected_digest)?;

    let (job_config, _job_digest) = pbs_config::host_backup::config()?;
    let jobs: Vec<pbsx_api_types::HostBackupJobConfig> =
        job_config.convert_to_typed_array("host-backup")?;
    if jobs.iter().any(|job| job.host == name) {
        bail!(
            "host '{}' still has host backup jobs; delete or retarget those jobs first",
            name
        );
    }

    if config.sections.remove(&name).is_none() {
        bail!("host '{}' does not exist.", name);
    }

    let (mut acl, _digest) = pbs_config::acl::config()?;
    acl.delete_node(&format!("/host/{name}"));
    pbs_config::acl::save_config(&acl)?;

    pbs_config::host::save_config(&config)?;
    crate::server::host::remove_host_status(&name)?;
    Ok(())
}

#[api(
    protected: true,
    input: {
        properties: {
            name: { schema: HOST_ID_SCHEMA },
        },
    },
    returns: { type: HostEnrollmentToken },
    access: {
        permission: &Permission::Privilege(&["host", "{name}"], PRIV_HOST_MODIFY, false),
    },
)]
/// Generate a short-lived one-time enrollment token for a host.
pub fn generate_enrollment_token(name: String) -> Result<HostEnrollmentToken, Error> {
    let (config, _digest) = pbs_config::host::config()?;
    let host: HostConfig = config.lookup("host", &name)?;
    if !host.enable {
        bail!("host '{}' is disabled", name);
    }
    crate::server::host::create_enrollment_token(&name)
}

const ENROLLMENT_TOKEN_ROUTER: Router = Router::new().post(&API_METHOD_GENERATE_ENROLLMENT_TOKEN);

const ITEM_SUBDIRS: proxmox_router::SubdirMap = &[("enrollment-token", &ENROLLMENT_TOKEN_ROUTER)];

const ITEM_ROUTER: Router = Router::new()
    .get(&API_METHOD_READ_HOST)
    .put(&API_METHOD_UPDATE_HOST)
    .delete(&API_METHOD_DELETE_HOST)
    .subdirs(ITEM_SUBDIRS);

pub const ROUTER: Router = Router::new()
    .get(&API_METHOD_LIST_HOSTS)
    .post(&API_METHOD_CREATE_HOST)
    .match_all("name", &ITEM_ROUTER);
