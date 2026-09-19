//! API types which belong to PBS-X rather than upstream PBS.
//!
//! Upstream PBS 4.x moved `pbs-api-types` into a separately packaged crate. Keeping PBS-X-only
//! types here avoids carrying a private fork of that upstream crate and makes future rebases much
//! less invasive.

mod acl;
mod host;

pub use acl::*;
pub use host::*;
