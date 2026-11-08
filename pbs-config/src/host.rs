use std::collections::HashMap;
use std::sync::LazyLock;

use anyhow::Error;
use proxmox_product_config::replace_privileged_config;
use proxmox_schema::{ApiType, Schema};
use proxmox_section_config::{SectionConfig, SectionConfigData, SectionConfigPlugin};

use pbsx_api_types::{HOST_ID_SCHEMA, HostConfig};

use crate::{BackupLockGuard, open_backup_lockfile};

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

/// Normalize an ASCII host ID without dropping characters from long IDs.
/// Subsequent components have an underscore prefix so punctuation at a split
/// cannot produce an invalid namespace component.
pub fn default_enrollment_namespace(host: &str) -> Result<pbs_api_types::BackupNamespace, Error> {
    if !host.is_ascii() {
        anyhow::bail!("host IDs must be ASCII");
    }
    let name = host.to_ascii_lowercase();
    let (first, rest) = name.split_at(name.len().min(32));
    let mut parts = vec![first.to_string()];
    for chunk in rest.as_bytes().chunks(31) {
        parts.push(format!("_{}", std::str::from_utf8(chunk)?));
    }
    pbs_api_types::BackupNamespace::new(&parts.join("/"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn enrollment_namespace_preserves_long_host_identity() {
        for host in [
            "S20-FE",
            "Office_PC.example",
            &format!("{}-{}", "a".repeat(32), "B".repeat(31)),
        ] {
            let ns = default_enrollment_namespace(host).unwrap();
            assert!(ns.components().all(|part| part.len() <= 32));
            let mut parts = ns.components();
            let mut restored = parts.next().unwrap().to_string();
            for part in parts {
                restored.push_str(part.strip_prefix('_').unwrap());
            }
            assert_eq!(restored, host.to_ascii_lowercase());
        }
        assert_eq!(
            default_enrollment_namespace("S20-FE").unwrap().to_string(),
            "s20-fe"
        );
    }
}

/// Shell completion helper for host IDs.
pub fn complete_host_name(_arg: &str, _param: &HashMap<String, String>) -> Vec<String> {
    match config() {
        Ok((data, _digest)) => data.sections.keys().map(|id| id.to_string()).collect(),
        Err(_) => Vec::new(),
    }
}
