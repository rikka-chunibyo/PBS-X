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
keeps the original `backup-id` separately from the destination `host-id`.
Neither the source identity nor its snapshot history is renamed or adopted.

There are two authorization checks:

1. The operator queueing the restore needs `Host.Modify` on the destination and
   `Datastore.Read` on the selected datastore/namespace.
2. The destination's storage API key must be authorized by the PBS reader to
   access the source group. A suitable read key can access multiple groups;
   an automatic device key remains restricted to its own group.

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
