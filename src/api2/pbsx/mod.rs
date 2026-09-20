//! PBS-X specific API endpoints.

pub mod agent;

use proxmox_router::{Router, SubdirMap, list_subdirs_api_method};

const SUBDIRS: SubdirMap = &[("agent", &agent::ROUTER)];

pub const ROUTER: Router = Router::new()
    .get(&list_subdirs_api_method!(SUBDIRS))
    .subdirs(SUBDIRS);
