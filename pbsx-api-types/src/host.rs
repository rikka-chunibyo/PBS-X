use serde::{Deserialize, Serialize};

use proxmox_schema::{Schema, StringSchema, Updater, api};

use pbs_api_types::{
    BACKUP_NAMESPACE_SCHEMA, BACKUP_TIME_SCHEMA, BackupNamespace, DATASTORE_SCHEMA, JOB_ID_SCHEMA,
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
            description: "Feature identifiers advertised by the agent.",
            items: {
                type: String,
                description: "Agent capability identifier.",
            },
        },
        disks: {
            type: Array,
            optional: true,
            description: "Disk inventory reported by the agent.",
            items: {
                type: HostDiskInfo,
            },
        },
        filesystems: {
            type: Array,
            optional: true,
            description: "Filesystem inventory reported by the agent.",
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

#[api(
    properties: {
        token: {
            type: String,
            description: "One-time PBS-X host enrollment token.",
        },
        expires: {
            type: i64,
            description: "Unix timestamp at which the enrollment token expires.",
        },
        "server-fingerprint": {
            type: String,
            description: "SHA-256 fingerprint of the PBS-X HTTPS certificate.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// One-time credential used to enroll a PBS-X host agent.
pub struct HostEnrollmentToken {
    pub token: String,
    pub expires: i64,
    pub server_fingerprint: String,
}

#[api(
    properties: {
        "host-id": { schema: HOST_ID_SCHEMA },
        token: {
            type: String,
            description: "One-time host enrollment token generated by PBS-X.",
        },
        "client-certificate": {
            type: String,
            description: "PEM client certificate generated by the agent.",
        },
        heartbeat: { type: HostAgentHeartbeat },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Public bootstrap request used once to bind an agent identity to a configured PBS-X host.
pub struct HostBootstrapRequest {
    pub host_id: String,
    pub token: String,
    pub client_certificate: String,
    pub heartbeat: HostAgentHeartbeat,
}

#[api(
    properties: {
        "backup-auth-id": {
            type: String,
            description: "PBS API token auth ID used only for native host backup uploads.",
        },
        "backup-secret": {
            type: String,
            description: "PBS API token secret. Returned only during enrollment/rotation.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Credentials returned after successful one-time host enrollment.
pub struct HostBootstrapResponse {
    pub backup_auth_id: String,
    pub backup_secret: String,
}

pub const HOST_BACKUP_SCHEDULE_SCHEMA: Schema =
    StringSchema::new("Run host backup job at specified schedule.")
        .format(&proxmox_schema::ApiStringFormat::VerifyFn(
            proxmox_time::verify_calendar_event,
        ))
        .type_text("<calendar-event>")
        .schema();

#[api]
#[derive(Clone, Copy, Debug, Default, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// How PBS-X should obtain a consistent view of a host source.
pub enum HostSnapshotMode {
    /// Select the best available provider (VSS, ZFS, Btrfs, LVM, then direct fallback).
    #[default]
    Auto,
    /// Require a point-in-time snapshot provider and fail if one is unavailable.
    Snapshot,
    /// Read the source directly without creating a filesystem snapshot.
    Direct,
}

#[api]
#[derive(Clone, Copy, Debug, Default, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// Change detection policy passed to the first-generation server-side PBS client.
pub enum HostChangeDetectionMode {
    /// Reuse files based on metadata where possible.
    #[default]
    Metadata,
    /// Compare file data before deciding whether content changed.
    Data,
    /// Use the legacy PBS client behavior.
    Legacy,
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
            description: "Agent-visible filesystem, volume, or path sources to protect.",
            items: {
                type: String,
                description: "Agent-visible filesystem, volume or path to protect.",
            },
        },
        excludes: {
            type: Array,
            optional: true,
            description: "Source subtrees excluded from this backup job.",
            items: {
                type: String,
                description: "Absolute or source-relative subtree path to exclude.",
            },
        },
        schedule: {
            optional: true,
            schema: HOST_BACKUP_SCHEDULE_SCHEMA,
        },
        "snapshot-mode": {
            type: HostSnapshotMode,
            optional: true,
        },
        "change-detection-mode": {
            type: HostChangeDetectionMode,
            optional: true,
        },
        enable: {
            type: bool,
            optional: true,
            default: true,
            description: "Whether this host backup job is enabled.",
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
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    #[updater(serde(skip_serializing_if = "Option::is_none"))]
    pub excludes: Vec<String>,
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
        "run-id": {
            type: String,
            description: "Unique PBS-X host backup run identifier.",
        },
        "job-id": { schema: JOB_ID_SCHEMA },
        "host-id": { schema: HOST_ID_SCHEMA },
        store: { schema: DATASTORE_SCHEMA },
        ns: {
            optional: true,
            schema: BACKUP_NAMESPACE_SCHEMA,
        },
        sources: {
            type: Array,
            description: "Agent-visible sources included in this backup run.",
            items: {
                type: String,
                description: "Agent-visible source path to protect.",
            },
        },
        excludes: {
            type: Array,
            optional: true,
            description: "Source subtrees excluded from this backup run.",
            items: {
                type: String,
                description: "Absolute or source-relative subtree path to exclude.",
            },
        },
        "snapshot-mode": { type: HostSnapshotMode },
        "change-detection-mode": { type: HostChangeDetectionMode },
        "requested-at": {
            type: i64,
            description: "Unix timestamp when PBS-X queued this run.",
        },
        "backup-time": {
            schema: BACKUP_TIME_SCHEMA,
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Self-contained backup command claimed by a PBS-X agent.
pub struct HostBackupRunCommand {
    pub run_id: String,
    pub job_id: String,
    pub host_id: String,
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    pub sources: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub excludes: Vec<String>,
    pub snapshot_mode: HostSnapshotMode,
    pub change_detection_mode: HostChangeDetectionMode,
    pub requested_at: i64,
    /// Server-assigned PBS snapshot timestamp. Stable across retries.
    pub backup_time: i64,
}

#[api(
    properties: {
        "host-id": { schema: HOST_ID_SCHEMA },
        "run-id": {
            type: String,
            description: "Unique PBS-X host backup run identifier.",
        },
        success: {
            type: bool,
            description: "Whether the host backup run completed successfully.",
        },
        error: {
            optional: true,
            type: String,
            description: "Failure message reported by the agent.",
        },
        "backup-time": {
            optional: true,
            type: i64,
            description: "PBS snapshot timestamp produced by the completed run.",
        },
        files: {
            optional: true,
            type: u64,
            description: "Number of files processed by the completed run.",
        },
        directories: {
            optional: true,
            type: u64,
            description: "Number of directories processed by the completed run.",
        },
        "logical-bytes": {
            optional: true,
            type: u64,
            description: "Logical bytes processed by the completed run.",
        },
        "duration-ms": {
            optional: true,
            type: u64,
            description: "Run duration in milliseconds.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Completion payload submitted by the agent after a native host backup run.
pub struct HostBackupRunResult {
    pub host_id: String,
    pub run_id: String,
    pub success: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub backup_time: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub files: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub directories: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub logical_bytes: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub duration_ms: Option<u64>,
}

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// PBS-X host backup orchestration state. Datastore snapshots remain the backup truth.
pub enum HostBackupRunState {
    /// The run is waiting for an enrolled agent to claim it.
    Queued,
    /// An enrolled agent has claimed and is executing the run.
    Running,
    /// The run has completed and its result is in history.
    Finished,
}

#[api(
    properties: {
        "host-id": { schema: HOST_ID_SCHEMA },
        "run-id": {
            type: String,
            description: "Unique PBS-X host backup run identifier.",
        },
        stage: {
            optional: true,
            type: String,
            description: "Current agent-reported backup stage.",
        },
        "processed-bytes": {
            type: u64,
            optional: true,
            default: 0,
            description: "Logical bytes processed so far.",
        },
        "uploaded-bytes": {
            type: u64,
            optional: true,
            default: 0,
            description: "Bytes uploaded so far.",
        },
        "processed-chunks": {
            type: u64,
            optional: true,
            default: 0,
            description: "Chunks processed so far.",
        },
        "uploaded-chunks": {
            type: u64,
            optional: true,
            default: 0,
            description: "Chunks uploaded so far.",
        },
        files: {
            type: u64,
            optional: true,
            default: 0,
            description: "Files processed so far.",
        },
    },
)]
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Progress submitted by the enrolled agent for a currently claimed run.
pub struct HostBackupRunProgress {
    pub host_id: String,
    pub run_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stage: Option<String>,
    #[serde(default)]
    pub processed_bytes: u64,
    #[serde(default)]
    pub uploaded_bytes: u64,
    #[serde(default)]
    pub processed_chunks: u64,
    #[serde(default)]
    pub uploaded_chunks: u64,
    #[serde(default)]
    pub files: u64,
}

#[api(
    properties: {
        "run-id": {
            type: String,
            description: "Unique PBS-X host backup run identifier.",
        },
        "job-id": { schema: JOB_ID_SCHEMA },
        "host-id": { schema: HOST_ID_SCHEMA },
        state: { type: HostBackupRunState },
        store: { schema: DATASTORE_SCHEMA },
        ns: { optional: true, schema: BACKUP_NAMESPACE_SCHEMA },
        "requested-at": {
            type: i64,
            description: "Unix timestamp when PBS-X queued this run.",
        },
        "backup-time": { schema: BACKUP_TIME_SCHEMA },
        "started-at": { optional: true, type: i64, description: "Unix timestamp when the agent claimed this run." },
        "updated-at": { optional: true, type: i64, description: "Unix timestamp of the latest progress update." },
        "finished-at": { optional: true, type: i64, description: "Unix timestamp when the run finished." },
        success: { optional: true, type: bool, description: "Whether the finished run succeeded." },
        error: { optional: true, type: String, description: "Failure message for an unsuccessful run." },
        stage: { optional: true, type: String, description: "Latest agent-reported backup stage." },
        "processed-bytes": { type: u64, description: "Logical bytes processed so far." },
        "uploaded-bytes": { type: u64, description: "Bytes uploaded so far." },
        "processed-chunks": { type: u64, description: "Chunks processed so far." },
        "uploaded-chunks": { type: u64, description: "Chunks uploaded so far." },
        files: { type: u64, description: "Files processed so far." },
        directories: { type: u64, description: "Directories processed so far." },
        "logical-bytes": { type: u64, description: "Logical bytes in the completed backup." },
        "duration-ms": { type: u64, description: "Run duration in milliseconds." },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Queue/execution history for one host backup run.
pub struct HostBackupRunInfo {
    pub run_id: String,
    pub job_id: String,
    pub host_id: String,
    pub state: HostBackupRunState,
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    pub requested_at: i64,
    pub backup_time: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub started_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub updated_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub finished_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub success: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stage: Option<String>,
    pub processed_bytes: u64,
    pub uploaded_bytes: u64,
    pub processed_chunks: u64,
    pub uploaded_chunks: u64,
    pub files: u64,
    pub directories: u64,
    pub logical_bytes: u64,
    pub duration_ms: u64,
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
            description: "Snapshot comment, if present.",
        },
        size: {
            optional: true,
            type: u64,
            description: "Snapshot size in bytes, if known.",
        },
        protected: {
            type: bool,
            description: "Whether the snapshot is protected from pruning.",
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

#[api]
#[derive(Clone, Copy, Debug, Default, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// Metadata behavior for a PBS-X host restore.
pub enum HostRestoreMode {
    /// Restore content and supported filesystem metadata.
    #[default]
    Normal,
    /// Restore content only, without applying archived metadata.
    NoAttr,
}

#[api(
    properties: {
        "run-id": {
            type: String,
            description: "Unique PBS-X host restore run identifier.",
        },
        "host-id": { schema: HOST_ID_SCHEMA },
        store: { schema: DATASTORE_SCHEMA },
        ns: { optional: true, schema: BACKUP_NAMESPACE_SCHEMA },
        "backup-time": { schema: BACKUP_TIME_SCHEMA },
        sources: {
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
        mode: { type: HostRestoreMode },
        "requested-at": {
            type: i64,
            description: "Unix timestamp when PBS-X queued this restore.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Durable restore command claimed by the enrolled host agent.
pub struct HostRestoreRunCommand {
    pub run_id: String,
    pub host_id: String,
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    pub backup_time: i64,
    pub sources: Vec<String>,
    pub destination: String,
    #[serde(default)]
    pub mode: HostRestoreMode,
    pub requested_at: i64,
}

#[api(
    properties: {
        "host-id": { schema: HOST_ID_SCHEMA },
        "run-id": { type: String, description: "Unique PBS-X host restore run identifier." },
        success: { type: bool, description: "Whether the host restore completed successfully." },
        error: { optional: true, type: String, description: "Failure message reported by the agent." },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Completion payload for a direct PBS-X host restore.
pub struct HostRestoreRunResult {
    pub host_id: String,
    pub run_id: String,
    pub success: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

#[api]
#[derive(Clone, Copy, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
/// PBS-X native host restore orchestration state.
pub enum HostRestoreRunState {
    /// The restore is waiting for an enrolled agent to claim it.
    Queued,
    /// An enrolled agent has claimed and is executing the restore.
    Running,
    /// The restore has completed and its result is in history.
    Finished,
}

#[api(
    properties: {
        "host-id": { schema: HOST_ID_SCHEMA },
        "run-id": { type: String, description: "Unique PBS-X host restore run identifier." },
        stage: { optional: true, type: String, description: "Current agent-reported restore stage." },
        "processed-bytes": { type: u64, optional: true, default: 0, description: "Logical bytes restored so far." },
        files: { type: u64, optional: true, default: 0, description: "Files restored so far." },
    },
)]
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Progress submitted by the enrolled agent for a currently claimed restore.
pub struct HostRestoreRunProgress {
    pub host_id: String,
    pub run_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stage: Option<String>,
    #[serde(default)]
    pub processed_bytes: u64,
    #[serde(default)]
    pub files: u64,
}

#[api(
    properties: {
        "run-id": { type: String, description: "Unique PBS-X host restore run identifier." },
        "host-id": { schema: HOST_ID_SCHEMA },
        state: { type: HostRestoreRunState },
        store: { schema: DATASTORE_SCHEMA },
        ns: { optional: true, schema: BACKUP_NAMESPACE_SCHEMA },
        "backup-time": { schema: BACKUP_TIME_SCHEMA },
        sources: {
            type: Array,
            description: "Archive paths selected for this restore.",
            items: { type: String, description: "Archive path selected for restore." },
        },
        destination: { type: String, description: "Destination directory on the protected host." },
        mode: { type: HostRestoreMode },
        "requested-at": { type: i64, description: "Unix timestamp when PBS-X queued this restore." },
        "started-at": { optional: true, type: i64, description: "Unix timestamp when the agent claimed this restore." },
        "updated-at": { optional: true, type: i64, description: "Unix timestamp of the latest restore progress update." },
        "finished-at": { optional: true, type: i64, description: "Unix timestamp when the restore finished." },
        success: { optional: true, type: bool, description: "Whether the finished restore succeeded." },
        error: { optional: true, type: String, description: "Failure message for an unsuccessful restore." },
        stage: { optional: true, type: String, description: "Latest agent-reported restore stage." },
        "processed-bytes": { type: u64, description: "Logical bytes restored so far." },
        files: { type: u64, description: "Files restored so far." },
        "duration-ms": { type: u64, description: "Restore duration in milliseconds." },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// Queue/execution history for one native host restore.
pub struct HostRestoreRunInfo {
    pub run_id: String,
    pub host_id: String,
    pub state: HostRestoreRunState,
    pub store: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ns: Option<BackupNamespace>,
    pub backup_time: i64,
    pub sources: Vec<String>,
    pub destination: String,
    pub mode: HostRestoreMode,
    pub requested_at: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub started_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub updated_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub finished_at: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub success: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub stage: Option<String>,
    pub processed_bytes: u64,
    pub files: u64,
    pub duration_ms: u64,
}
