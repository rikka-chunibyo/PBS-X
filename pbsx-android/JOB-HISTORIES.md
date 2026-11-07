# Job identity and backup histories

Implemented in Android 0.3.0 with the matching PBS-X server update.

The local job UUID identifies a schedule and its source selections. **Job name**
is a cosmetic label included in snapshot notes. **Datastore**, **Namespace** and
**Backup ID** together identify the PBS `host` backup group and its history.

New jobs default to `devices/<device-id>` and a stable generated Backup ID. Both
fields are editable. Renaming the job or changing its schedule does not change
the Backup ID. Pressing Back up manually and scheduled runs of that job use the
same destination. Use existing backup selects a visible host group's datastore,
namespace and Backup ID without changing the enrolled device identity.

Two different jobs can deliberately use the same destination. They share the
snapshot list; contents are never merged. Every snapshot is a complete view of
only that run's selected folders and Shizuku sources. Different Backup IDs have
separate histories, while PBS can deduplicate their chunks within the datastore.
Changing a destination starts or selects another history. Earlier groups remain
available through Restore; no move, rename or deletion is performed.

Existing jobs without an explicit Backup ID retain the old device-ID fallback
and configured namespace. Editing them materializes that ID in the form, so an
upgrade alone does not switch their backup history.

## Device keys and namespaces

New server host jobs suggest the same `devices/<host-id>` namespace. Saving a
server job can provision missing namespace levels, using normal administrator
`Datastore.Modify` checks. Devices themselves receive only `DatastoreBackup` on
the approved namespace, without propagation or namespace creation privileges.
The root namespace remains an explicit option for administrators who want it.

Generated device keys are normal PBS API tokens, visible under Access Control
and usable with administrator-added namespace ACLs. The upload/reader no longer
requires the Backup ID to match the device ID or requires an enabled server job
when a manual ACL already authorizes access. Ordinary PBS ACLs and ownership
checks remain in force. A read-only grant does not authorize appending snapshots
to another owner's group.

Automatic ACL synchronization tracks the grants it manages. Editing or deleting
a server job removes those grants while preserving grants on other namespaces
and other roles added manually. Administrator-enabled propagation is preserved.
Legacy credential records initialize this tracking before their next job edit.
Retained and replacement enrollment keys still recognize earlier owners from
the same device after normal namespace permission checks. Only the newest key
controls queued device work. Revoke existing keys remains optional and false by
default.

Desktop Windows/Linux jobs still use the host ID as their default Backup ID;
this version adds editable Backup IDs to Android's local jobs. Their existing
agents remain compatible with the namespace ACL model.

## Server host backup listing

Host → Backups includes accessible host groups in `devices/<device-id>` and its
descendants, groups owned by the device's current/retained/retired generated
keys in any namespace, and legacy groups with the device ID. Long device IDs
use the same 32-character namespace components as the job editor. Disabling,
retargeting or deleting a server job does not hide those historical snapshots.
The operator still needs Host.Audit and normal PBS namespace/owner access.

Each row includes the actual Backup ID, which is sent independently of the
source/destination device IDs when queueing a desktop-agent restore. Omitted
Backup IDs retain the older device-ID fallback for API compatibility. Android
restores continue to run locally in the app.

For a manually managed/shared key, use the device namespace when you want
custom IDs to appear under that device. Outside that namespace, such a shared
key does not identify which device produced a group; the permission-derived
global catalogue and Datastore → Content remain the complete views.
