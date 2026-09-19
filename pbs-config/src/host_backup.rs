use std::collections::HashMap;

use anyhow::Error;
use lazy_static::lazy_static;

use proxmox_schema::{ApiType, Schema};
use proxmox_section_config::{SectionConfig, SectionConfigData, SectionConfigPlugin};

use pbs_api_types::JOB_ID_SCHEMA;
use pbsx_api_types::HostBackupJobConfig;

use crate::{open_backup_lockfile, replace_backup_config, BackupLockGuard};

lazy_static! {
    pub static ref CONFIG: SectionConfig = init();
}

fn init() -> SectionConfig {
    let obj_schema = match HostBackupJobConfig::API_SCHEMA {
        Schema::Object(ref object_schema) => object_schema,
        _ => unreachable!(),
    };

    let plugin = SectionConfigPlugin::new(
        "host-backup".to_string(),
        Some("id".to_string()),
        obj_schema,
    );
    let mut config = SectionConfig::new(&JOB_ID_SCHEMA);
    config.register_plugin(plugin);
    config
}

pub const HOST_BACKUP_CFG_FILENAME: &str = "/etc/proxmox-backup/host-backup.cfg";
pub const HOST_BACKUP_CFG_LOCKFILE: &str = "/etc/proxmox-backup/.host-backup.lck";

pub fn lock_config() -> Result<BackupLockGuard, Error> {
    open_backup_lockfile(HOST_BACKUP_CFG_LOCKFILE, None, true)
}

pub fn config() -> Result<(SectionConfigData, [u8; 32]), Error> {
    let content =
        proxmox_sys::fs::file_read_optional_string(HOST_BACKUP_CFG_FILENAME)?.unwrap_or_default();
    let digest = openssl::sha::sha256(content.as_bytes());
    let data = CONFIG.parse(HOST_BACKUP_CFG_FILENAME, &content)?;
    Ok((data, digest))
}

pub fn save_config(config: &SectionConfigData) -> Result<(), Error> {
    let raw = CONFIG.write(HOST_BACKUP_CFG_FILENAME, config)?;
    replace_backup_config(HOST_BACKUP_CFG_FILENAME, raw.as_bytes())
}

pub fn complete_host_backup_job_id(
    _arg: &str,
    _param: &HashMap<String, String>,
) -> Vec<String> {
    match config() {
        Ok((data, _digest)) => data.sections.keys().map(|id| id.to_string()).collect(),
        Err(_) => Vec::new(),
    }
}
