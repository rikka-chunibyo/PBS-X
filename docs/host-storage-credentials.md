# Host storage credentials and restores

PBS-X hosts keep a stable identity for inventory and job delivery. That identity
does not need to move to a replacement computer to restore its backups.

## Setup

The Agent enrollment dialog offers two storage modes:

- **Automatic device key** (default): enrollment creates the same isolated,
  per-device API token used previously. Suitable for family devices.
- **Existing PBS API key**: enter `user@realm!token` and its secret. The agent
  uses this credential for datastore uploads and reads. Ordinary PBS permissions
  and backup-group ownership determine which backups it can access.

The automatic device key is still created in custom mode, because heartbeats,
job claims and completion reports must stay isolated to that device. A storage
key with broad datastore access does not grant control of other PBS-X hosts.

Custom credentials are stored as one encrypted entry using the existing Windows
DPAPI or Linux encrypted configuration backend. They are not saved in host or job
configuration on the server. An enrollment code that contains a custom key is a
long-lived credential: handle it as a secret, not merely a short-lived token.

To change the storage key locally after setup, run the following as administrator
or root. The secret is prompted without echo on a terminal; noninteractive use
reads it from stdin. Do not put the secret in command arguments.

```text
pbsx-host-agent storage-key user@pbs!token
pbsx-host-agent storage-key auto
```

Changes apply to the next backup or restore. An operation already underway keeps
the credentials it started with. Missing or invalid custom credentials fail the
operation; they never retry using the automatic key.

Changing credentials does not transfer ownership of existing backup groups.
When switching the key used for ongoing backups, the group's owner must permit
that key under normal PBS rules; use PBS's existing Change Owner action if
needed. Returning to automatic mode does not undo ownership changes.

## Restore onto another host

Select a snapshot under the original host's Backups tab, choose Restore, select
the destination host and enter a directory on that destination. The command
keeps the selected snapshot's actual `backup-id` separately from the destination
`host-id`, including custom Android histories.
Neither the source identity nor its snapshot history is renamed or adopted.

There are two authorization checks:

1. The operator queueing the restore needs `Host.Modify` on the destination and
   `Datastore.Read` on the selected datastore/namespace.
2. The destination's storage API key must be authorized by the PBS reader to
   access the source group. A suitable read key can access multiple groups;
   an automatic device key is initially limited to its approved namespace.
   Administrators can extend its normal PBS ACLs in Permissions.

This permits restoring backups whose original computer is offline. It does not
give devices unrestricted access merely because they appear in the Host list.
Windows local account SIDs and Linux UID/GID values may differ on a replacement
computer; choose Content only when archived ownership and permissions should not
be applied. This is file restore, not image/bare-metal recovery.

Old enrollment codes and same-host restore commands remain supported. Upgrade
the destination agent and server together for cross-host restores.

## Validation

- Native Windows credential-selection, enrollment, and full module tests.
- Existing live Windows multi-source backup/restore suite passed again using
  automatic credentials after the storage-connection change.
- Linux credential/enrollment/backup package tests.
- Rust API-type compatibility test and server `cargo check --lib`.
- JavaScript dialog tests cover automatic/custom enrollment payloads, secret
  removal when returning to automatic mode, and destination-host selection.
- Live Windows test created a separate `host/pbsx-storage-credential-fixture`
  backup (timestamp `1793513737`), then restored its bytes onto `pbsx-win-114`.
  Automatic access was denied, authorized custom access succeeded, an invalid
  custom secret was denied, and management access continued independently.
- The live test restored automatic mode. Its temporary datastore-only token,
  ACL grants, and secret files on both test VMs were removed afterward.

The live credential test exercises the production agent runners and PBS reader;
the new server queue API and dialogs have build/unit validation but have not yet
been deployed for a complete browser-to-queue cross-host test.

## Optional revocation when generating enrollment

The Generate Enrollment popup has a **Revoke existing keys** checkbox,
**unchecked (`false`) by default**. Changing it regenerates the one-time code;
the Copy button stays disabled until the server returns the new grant. A failed
regeneration clears the displayed code and can be retried with Regenerate code.
Storage secrets are encoded locally and are never sent to the grant endpoint.

- **Unchecked:** enrollment issues another independent, narrowly scoped API
  token. Earlier generated keys retain backup and restore access to this host's
  configured destinations, including its existing snapshots.
- **Checked:** successful redemption revokes this host's earlier generated keys.
  Merely generating a code, abandoning it, or submitting an expired, invalid or
  already-used code does not revoke them. Independently managed API keys and
  other hosts' keys are unaffected.
- **Agent control:** only the newest enrollment can send this host's heartbeats
  or claim/report its queued jobs. Earlier retained keys have storage access
  only. This avoids multiple devices competing for one host's server jobs;
  another independently managed host should have its own host definition and
  can select an authorized storage key separately.

The API accepts optional `revoke-existing-keys` on
`POST /config/host/{host}/enrollment-token`; omission means `false`. The choice is
stored with the grant digest and enforced during bootstrap, not supplied by the
enrolling client. Existing agents and enrollment-code formats remain compatible.
The new web UI and server must be upgraded together.

Generated keys have distinct token IDs. The host's credential registry tracks
the current controller, retained keys and retired key IDs, without storing API
secrets. Old single-key records load without migration or re-enrollment. Job
edits refresh ACLs for all retained keys, and deleting a host revokes all its
generated keys. Normal PBS user/token owner checks remain unchanged for manual
credentials. Generated keys additionally recognize the same host's earlier
generated owners after normal namespace permission checks;
this keeps backups usable after rotation without changing their recorded owner.
No host IDs, backup groups or owners are adopted or renamed.

## Device namespaces and Android backup IDs

Generated keys appear under Access Control → API Tokens. Namespace ACLs are
authoritative for storage access, including grants added manually through
Permissions. The server no longer requires a generated key's backup ID to equal
its host ID or checks enabled server jobs after a manual ACL permits access.
PBS group ownership still applies; a read grant is not permission to append to
another owner's existing group.

New server job forms suggest `devices/<host-id>`. Long IDs are split into
32-character namespace components. Missing namespaces can be provisioned when
saving a job, with normal `Datastore.Modify` checks and backup-service filesystem
ownership. The device key receives `DatastoreBackup` only on the approved
namespace, without propagation or namespace-creation privileges. Clearing the
namespace explicitly selects root; existing job configurations are unchanged.

Automatic ACL synchronization records its managed paths rather than deleting
every ACL for the key. Job edits remove obsolete automatic grants and preserve
manually added namespaces, other roles and administrator-widened propagation.
Legacy records initialize tracking before their next configuration edit.

Android 0.3.0 separates cosmetic Job name from Backup ID. Its internal job UUID
continues to identify settings and scheduling. Jobs with equal datastore,
namespace and Backup ID share snapshots, without merging contents. Desktop
Windows/Linux orchestration still uses the host ID as its default Backup ID.
