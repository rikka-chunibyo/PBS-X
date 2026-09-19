use serde::{Deserialize, Serialize};

use proxmox_schema::{api, Schema, StringSchema, Updater};

use pbs_api_types::{
    BackupNamespace, BACKUP_NAMESPACE_SCHEMA, BACKUP_TIME_SCHEMA, DATASTORE_SCHEMA, JOB_ID_SCHEMA,
    PROXMOX_SAFE_ID_FORMAT, SINGLE_LINE_COMMENT_SCHEMA,
};

pub const HOST_ID_SCHEMA: Schema = StringSchema::new("PBS-X host ID.")
    .format(&PROXMOX_SAFE_ID_FORMAT)
    .min_length(2)
    .max_length(64)
    .schema();

fn return_true() -> bool {
    true
}

fn is_true(value: &bool) -> bool {
    *value
}

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// Operating system family reported by a PBS-X agent.
pub enum HostOperatingSystem {
    /// Linux host.
    Linux,
    /// Windows host.
    Windows,
    /// Any other or currently unknown operating system.
    Other,
}

#[api(
    properties: {
        name: {
            schema: HOST_ID_SCHEMA,
        },
        enable: {
            type: bool,
            optional: true,
            default: true,
        },
        comment: {
            optional: true,
            schema: SINGLE_LINE_COMMENT_SCHEMA,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Updater)]
#[serde(rename_all = "kebab-case")]
/// Static configuration for a PBS-X protected host.
pub struct HostConfig {
    #[updater(skip)]
    pub name: String,
    #[serde(default = "return_true", skip_serializing_if = "is_true")]
    #[updater(serde(skip_serializing_if = "Option::is_none"))]
    /// Whether the host is allowed to connect and run host operations.
    pub enable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    /// Optional administrator-facing note.
    pub comment: Option<String>,
}

#[api(
    properties: {
        name: {
            type: String,
            description: "OS-visible disk name or device path.",
        },
        size: {
            type: u64,
            optional: true,
            description: "Disk size in bytes, if known.",
        },
        model: {
            type: String,
            optional: true,
            description: "Disk model, if known.",
        },
        serial: {
            type: String,
            optional: true,
            description: "Disk serial number, if known.",
        },
        rotational: {
            type: bool,
            optional: true,
            description: "Whether the device is rotational media, if known.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Disk inventory item reported by an agent.
pub struct HostDiskInfo {
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub size: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub serial: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub rotational: Option<bool>,
}

#[api(
    properties: {
        mountpoint: {
            type: String,
            description: "Filesystem mount point or Windows volume path.",
        },
        "file-system": {
            type: String,
            description: "Filesystem type, if known.",
        },
        source: {
            type: String,
            optional: true,
            description: "Backing device, volume, dataset, or other source.",
        },
        total: {
            type: u64,
            optional: true,
            description: "Total filesystem size in bytes, if known.",
        },
        available: {
            type: u64,
            optional: true,
            description: "Available filesystem space in bytes, if known.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Filesystem inventory item reported by an agent.
pub struct HostFilesystemInfo {
    pub mountpoint: String,
    pub file_system: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub total: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub available: Option<u64>,
}

#[api(
    properties: {
        hostname: {
            type: String,
            description: "Hostname reported by the agent.",
        },
        "operating-system": {
            type: HostOperatingSystem,
        },
        "os-version": {
            type: String,
            optional: true,
            description: "Human-readable operating system version.",
        },
        architecture: {
            type: String,
            description: "CPU/OS architecture reported by the agent.",
        },
        "agent-version": {
            type: String,
            description: "PBS-X agent version.",
        },
        capabilities: {
            type: Array,
            optional: true,
            items: {
                type: String,
            },
        },
        disks: {
            type: Array,
            optional: true,
            items: {
                type: HostDiskInfo,
            },
        },
        filesystems: {
            type: Array,
            optional: true,
            items: {
                type: HostFilesystemInfo,
            },
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Agent-supplied heartbeat and inventory payload.
pub struct HostAgentHeartbeat {
    pub hostname: String,
    pub operating_system: HostOperatingSystem,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub os_version: Option<String>,
    pub architecture: String,
    pub agent_version: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub capabilities: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub disks: Vec<HostDiskInfo>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub filesystems: Vec<HostFilesystemInfo>,
}

#[api(
    properties: {
        heartbeat: {
            type: HostAgentHeartbeat,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Last agent status persisted by the server.
pub struct HostAgentStatus {
    /// Server-side timestamp of the last successful heartbeat.
    pub last_seen: i64,
    #[serde(flatten)]
    pub heartbeat: HostAgentHeartbeat,
}

#[api(
    properties: {
        config: {
            type: HostConfig,
        },
        status: {
            type: HostAgentStatus,
            optional: true,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Host configuration combined with current connectivity information.
pub struct HostListItem {
    #[serde(flatten)]
    pub config: HostConfig,
    /// True if the most recent heartbeat is still considered fresh.
    pub online: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<HostAgentStatus>,
}

pub const HOST_BACKUP_SCHEDULE_SCHEMA: Schema = StringSchema::new("Run host backup job at specified schedule.")
    .format(&proxmox_schema::ApiStringFormat::VerifyFn(
        proxmox_time::verify_calendar_event,
    ))
    .type_text("<calendar-event>")
    .schema();

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// How PBS-X should obtain a consistent view of a host source.
pub enum HostSnapshotMode {
    /// Select the best available provider (VSS, ZFS, Btrfs, LVM, then direct fallback).
    Auto,
    /// Require a point-in-time snapshot provider and fail if one is unavailable.
    Snapshot,
    /// Read the source directly without creating a filesystem snapshot.
    Direct,
}

impl Default for HostSnapshotMode {
    fn default() -> Self {
        Self::Auto
    }
}

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// Change detection policy passed to the first-generation server-side PBS client.
pub enum HostChangeDetectionMode {
    /// Reuse files based on metadata where possible.
    Metadata,
    /// Compare file data before deciding whether content changed.
    Data,
    /// Use the legacy PBS client behavior.
    Legacy,
}

impl Default for HostChangeDetectionMode {
    fn default() -> Self {
        Self::Metadata
    }
}

#[api(
    properties: {
        id: {
            schema: JOB_ID_SCHEMA,
        },
        host: {
            schema: HOST_ID_SCHEMA,
        },
        store: {
            schema: DATASTORE_SCHEMA,
        },
        ns: {
            optional: true,
            schema: BACKUP_NAMESPACE_SCHEMA,
        },
        sources: {
            type: Array,
            items: {
                type: String,
                description: "Agent-visible filesystem, volume or path to protect.",
            },
        },
        schedule: {
            optional: true,
            schema: HOST_BACKUP_SCHEDULE_SCHEMA,
        },
        "snapshot-mode": {
            type: HostSnapshotMode,
            optional: true,
            default: "auto",
        },
        "change-detection-mode": {
            type: HostChangeDetectionMode,
            optional: true,
            default: "metadata",
        },
        enable: {
            type: bool,
            optional: true,
            default: true,
        },
        comment: {
            optional: true,
            schema: SINGLE_LINE_COMMENT_SCHEMA,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Updater)]
#[serde(rename_all = "kebab-case")]
/// Native PBS-X backup job for a registered Windows or Linux host.
pub struct HostBackupJobConfig {
    #[updater(skip)]
    pub id: String,
    pub host: String,
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    #[serde(default)]
    pub sources: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub schedule: Option<String>,
    #[serde(default)]
    pub snapshot_mode: HostSnapshotMode,
    #[serde(default)]
    pub change_detection_mode: HostChangeDetectionMode,
    #[serde(default = "return_true", skip_serializing_if = "is_true")]
    #[updater(serde(skip_serializing_if = "Option::is_none"))]
    pub enable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub comment: Option<String>,
}

impl HostBackupJobConfig {
    /// PBS backup identity is the host, never the job ID.
    pub fn backup_id(&self) -> &str {
        &self.host
    }

    pub fn datastore_acl_path(&self) -> Vec<&str> {
        match self.ns.as_ref() {
            Some(ns) => ns.acl_path(&self.store),
            None => vec!["datastore", &self.store],
        }
    }
}

#[api(
    properties: {
        store: {
            schema: DATASTORE_SCHEMA,
        },
        ns: {
            optional: true,
            schema: BACKUP_NAMESPACE_SCHEMA,
        },
        "backup-time": {
            schema: BACKUP_TIME_SCHEMA,
        },
        comment: {
            optional: true,
            type: String,
        },
        size: {
            optional: true,
            type: u64,
        },
        protected: {
            type: bool,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// A normal PBS host snapshot associated with a registered PBS-X host.
pub struct HostSnapshotListItem {
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    pub backup_time: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub comment: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub size: Option<u64>,
    pub protected: bool,
}

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// ZFS pool layouts accepted by PBS-X. `Raid0` is a normal ZFS stripe with multiple top-level disks.
pub enum PbsxZfsRaidLevel {
    /// Single disk pool.
    Single,
    /// Striped pool with no redundancy.
    Raid0,
    /// Two-or-more disk mirror vdev.
    Mirror,
    /// Stripe across two-disk mirror vdevs.
    Raid10,
    /// Single-parity RAIDZ.
    RaidZ,
    /// Double-parity RAIDZ2.
    RaidZ2,
    /// Triple-parity RAIDZ3.
    RaidZ3,
}
