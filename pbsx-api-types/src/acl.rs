//! PBS-X ACL additions.
//!
//! PBS privileges are represented as a `u64` bitset. Upstream currently uses the low bits. PBS-X
//! deliberately reserves three high bits for host management so it can extend ACLs without
//! forking the separately packaged `pbs-api-types` crate.


use serde::{Deserialize, Serialize};

use proxmox_schema::api;

use pbs_api_types::{ACL_PATH_SCHEMA, ACL_PROPAGATE_SCHEMA};

#[api(
    properties: {
        path: { schema: ACL_PATH_SCHEMA },
        propagate: { schema: ACL_PROPAGATE_SCHEMA },
        "ugid-type": {
            type: String,
            description: "ACL identity type (user or group).",
        },
        ugid: {
            type: String,
            description: "User or group identifier.",
        },
        roleid: {
            type: String,
            description: "Role identifier, including PBS-X extension roles.",
        },
    },
)]
#[derive(Clone, Debug, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
/// ACL entry whose role schema can represent both upstream and PBS-X roles.
pub struct PbsxAclListItem {
    pub path: String,
    pub ugid: String,
    pub ugid_type: String,
    pub propagate: bool,
    pub roleid: String,
}

/// Host.Audit allows reading PBS-X host configuration, inventory and runtime status.
pub const PRIV_HOST_AUDIT: u64 = 1_u64 << 60;
/// Host.Modify allows creating, updating and deleting PBS-X hosts and host backup jobs.
pub const PRIV_HOST_MODIFY: u64 = 1_u64 << 61;
/// Host.Agent allows an enrolled agent to report state and service requests for its own host.
pub const PRIV_HOST_AGENT: u64 = 1_u64 << 62;

/// Host.Audit can inspect PBS-X host configuration and runtime status.
pub const ROLE_HOST_AUDIT: u64 = PRIV_HOST_AUDIT;
/// Host.Admin can manage PBS-X hosts and inspect their state.
pub const ROLE_HOST_ADMIN: u64 = PRIV_HOST_AUDIT | PRIV_HOST_MODIFY;
/// Host.Agent is intended for a host-scoped PBS-X agent API token.
pub const ROLE_HOST_AGENT: u64 = PRIV_HOST_AGENT;

/// Privilege names exposed by the PBS-X access API in addition to upstream PBS privileges.
pub const HOST_PRIVILEGES: &[(&str, u64)] = &[
    ("Host.Audit", PRIV_HOST_AUDIT),
    ("Host.Modify", PRIV_HOST_MODIFY),
    ("Host.Agent", PRIV_HOST_AGENT),
];

/// PBS-X role definitions appended to upstream's built-in roles.
pub const HOST_ROLES: &[(&str, u64, &str)] = &[
    (
        "HostAudit",
        ROLE_HOST_AUDIT,
        "PBS-X host auditor (view host configuration, inventory and status)",
    ),
    (
        "HostAdmin",
        ROLE_HOST_ADMIN,
        "PBS-X host administrator (view and manage protected hosts)",
    ),
    (
        "HostAgent",
        ROLE_HOST_AGENT,
        "PBS-X host agent (host-scoped agent communication)",
    ),
];
