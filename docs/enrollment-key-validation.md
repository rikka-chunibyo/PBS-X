# Enrollment key lifecycle validation — 2026-11-01

Implemented on branch `pbsx-enrollment-key-revocation` and deployed to PBS-X test
VM 104 (`10.0.1.21`). No hypervisor or development-VM lifecycle changes were made.

The enrollment popup defaults **Revoke existing keys** to false. The server
stores that choice with the one-time grant and applies it only on redemption.
Each enrollment issues a distinct API token. Earlier retained keys keep storage
access; only the newest enrollment controls the host's queued agent work.

## Passed

- Rust server compile check, three focused unit tests and builds of the API,
  proxy and debug executables. Tests cover legacy single-key/grant records,
  retained and retired key history, distinct token IDs and the full `user.cfg`
  identifier length limit (including `user@realm!`).
- Repository JavaScript lint and dialog checks: false default, explicit true and
  false regeneration, stale codes disabled during requests/after failure,
  retry, and keeping custom storage secrets out of grant API requests.
- Live PBS test: two independent generated keys backed up to the same existing
  group, listed snapshots and restored their bytes. Job edits removed and
  restored destination ACLs for both retained keys. The previous key lost
  agent-control access while retaining backup access.
- Live explicit revocation: both earlier keys were rejected with HTTP 401.
  The replacement key restored historical snapshots and appended another
  backup, including snapshot notes. The group's recorded owner stayed unchanged.
- An unrelated host's generated key and an independently managed reader token
  remained usable. Generated keys were denied access to another host's group.
  Expired, invalid and replayed one-time grants were rejected; issuing an unused
  revocation grant did not invalidate the active keys.
- A partial-creation failure exposed the full section-ID length constraint.
  Generated names now fit it while retaining 128 random bits. Failure cleanup
  also removes a shadow secret if token creation could not persist `user.cfg`.
  The failed disposable fixture's orphan secret was removed through the host
  credential cleanup path.
- The normal live PBS baseline passed again: three snapshots, zero new chunk
  bytes for unchanged data, named snapshot notes, directory browsing, current
  and historical restores, all selective conflict modes, and a separate
  destination identity.

The live tests use the real TLS API, PBS upload and reader protocols, and only
temporary `android-smoke-*` hosts/jobs/namespaces. Real device credentials stay
on the test VM. Fixture resources and the manual reader key are removed on exit;
existing phone/Windows keys are not rotated by the tests.
Final read-only checks confirmed no fixture hosts, jobs, namespaces, grants,
credential records, generated shadow entries or manual reader token remained.
Both PBS services are active, and the uploaded staging directory was removed.

## Reproduce

Build the Go integration test binary from `pbsx-android/core/mobilecore` using
`go test -c -tags integration`. On the disposable PBS test VM, run as root:

```sh
PBSX_ANDROID_TEST_PATTERN='^TestLiveEnrollmentKeys$' \
  bash test-live-pbs.sh /path/to/mobilecore-live.test
```

Omit the environment variable to run the existing `TestLivePBS` baseline.
The caller must supply the test VM's disposable datastore named `backup`.

## Deployment

The update checks for running tasks, verifies artifact hashes and dynamic
libraries, and retains verified executable/UI copies before restarting only
the test VM's PBS services. Public index and served-JavaScript checks verify the
running UI. Original files from before this feature remain at
`/var/backups/pbsx-enrollment-preview-20261101-190547.ShSyST` on VM 104.

The initial health probes used authenticated/nonexistent URLs and automatically
restored the original files. The final probes use the paths declared in the UI
template and pass. Existing client binaries need no enrollment-format change;
the web UI and server are upgraded together.
