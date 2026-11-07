# Host backup-history validation — 2026-11-07

Host → Backups now discovers accessible PBS host histories independently of
enabled server jobs. Association includes the standard device namespace and
its descendants, groups owned by current or historical generated device keys,
and legacy groups whose Backup ID equals the device ID. Normal namespace and
group-owner filtering still applies before a snapshot is returned. Unmounted
removable datastores are skipped; other datastore/read errors fail visibly.

Rows expose the actual Backup ID. Restore forwards that ID independently of
the selected destination host. The API's optional `backup-id` parameter falls
back to the source device ID when omitted, preserving existing callers.
Manually managed/shared keys outside the standard device namespace do not
establish a device association for arbitrary IDs; those groups remain available
through Datastore → Content and Android's permission-derived global catalogue.

## Validation

- Seven focused server Rust tests passed: two history-association cases and
  five enrollment/ACL regressions. They include retained/retired generated
  owners, unrelated owners, legacy IDs, namespace component boundaries and
  64-character device IDs split into valid namespace components.
- All 12 `pbs-config` tests and the `pbsx-api-types` compatibility test passed.
  API, proxy and debug executables built successfully. Web bundle assembly,
  changed-file JavaScript lint, syntax and dialog/controller tests passed.
- Expanded `TestLiveBackupIdentities` passed on test VM 104. Real uploads and
  historical restores verify shared/separate Backup IDs without merging,
  manually granted and nested namespaces, retained-key ownership, and host-page
  visibility after disabling a job. An unrelated owner's group is excluded.
- A namespace-only operator can list the associated group without parent/root
  access. Removing its read role reinstates owner filtering; Host.Agent alone
  cannot list the host page and an audit-only operator cannot queue restores.
- Custom and legacy restore requests retain the expected queued Backup ID.
  A manual-key backup in the device namespace remains visible after deleting
  its server job. UI tests preserve selected IDs when timestamps coincide.
- `TestLivePBS` and `TestLiveEnrollmentKeys` also passed using separate
  disposable fixtures: deduplication, selective/conflict-mode restore,
  historical snapshots, retained/revoked keys and manual credentials.

The first expanded run caught a test-only job name exceeding PBS's 32-character
limit. Its suffix was shortened; the complete rerun passed. No product-code
change was needed for that failure.

## Deployment and scope

Built on PBS-X VM 103 and installed only on test VM 104, with verified rollback
files retained at `/var/backups/pbsx-host-histories-20261107-070815.KHSNIL`.
Deployment checked idle tasks and libraries, restarted only the test VM's PBS
services, and compared the served GUI with the built bundle. Installed binary
and GUI hashes were checked after testing. Disposable hosts, jobs, namespaces,
tokens, ACLs, queued commands, credentials and staging tools were removed.
VMs 103/104 were returned to their original stopped states and RAM/balloon
settings. The development VM, unrelated guests and hypervisor services were
not restarted or changed.

The Android production code/APK is unchanged by this fix; its preceding 29-test
instrumentation run is recorded in [Android validation](../pbsx-android/TESTING.md).
This pass exercised server APIs and the JavaScript controller harness rather
than an interactive browser or a desktop-agent execution of the queued restore.
No real-phone writes or provider exports were performed. Image backups,
device-data imports, encryption and Secure Folder remain separate work.
