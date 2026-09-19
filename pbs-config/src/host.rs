use std::collections::HashMap;
use std::sync::LazyLock;

use anyhow::Error;
use proxmox_schema::{ApiType, Schema};
use proxmox_product_config::replace_privileged_config;
use proxmox_section_config::{SectionConfig, SectionConfigData, SectionConfigPlugin};

use pbsx_api_types::{HostConfig, HOST_ID_SCHEMA};

use crate::{open_backup_lockfile, BackupLockGuard};

pub static CONFIG: LazyLock<SectionConfig> = LazyLock::new(init);

fn init() -> SectionConfig {
    let obj_schema = match HostConfig::API_SCHEMA {
        Schema::Object(ref object_schema) => object_schema,
        _ => unreachable!(),
    };

    let plugin = SectionConfigPlugin::new("host".to_string(), Some("name".to_string()), obj_schema);
    let mut config = SectionConfig::new(&HOST_ID_SCHEMA);
    config.register_plugin(plugin);
    config
}

pub const HOST_CFG_FILENAME: &str = "/etc/proxmox-backup/host.cfg";
pub const HOST_CFG_LOCKFILE: &str = "/etc/proxmox-backup/.host.lck";

/// Get an exclusive lock for the PBS-X host configuration.
pub fn lock_config() -> Result<BackupLockGuard, Error> {
    open_backup_lockfile(HOST_CFG_LOCKFILE, None, true)
}

/// Read the PBS-X host configuration and its SHA-256 digest.
pub fn config() -> Result<(SectionConfigData, [u8; 32]), Error> {
    let content =
        proxmox_sys::fs::file_read_optional_string(HOST_CFG_FILENAME)?.unwrap_or_default();

    let digest = openssl::sha::sha256(content.as_bytes());
    let data = CONFIG.parse(HOST_CFG_FILENAME, &content)?;
    Ok((data, digest))
}

/// Persist the PBS-X host configuration.
pub fn save_config(config: &SectionConfigData) -> Result<(), Error> {
    let raw = CONFIG.write(HOST_CFG_FILENAME, config)?;
    replace_privileged_config(HOST_CFG_FILENAME, raw.as_bytes())
}

/// Shell completion helper for host IDs.
pub fn complete_host_name(_arg: &str, _param: &HashMap<String, String>) -> Vec<String> {
    match config() {
        Ok((data, _digest)) => data.sections.keys().map(|id| id.to_string()).collect(),
        Err(_) => Vec::new(),
    }
}
