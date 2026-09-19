//! PBS-X specific API endpoints.

pub mod agent;

use proxmox_router::{list_subdirs_api_method, Router, SubdirMap};

const SUBDIRS: SubdirMap = &[("agent", &agent::ROUTER)];

pub const ROUTER: Router = Router::new()
    .get(&list_subdirs_api_method!(SUBDIRS))
    .subdirs(SUBDIRS);
