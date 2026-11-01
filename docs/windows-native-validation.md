# Native Windows validation

Validated on 2026-11-01 using Windows Server 2025 Evaluation, Go 1.27.3,
and the dedicated PBS-X VM 114 (`pbsx-win-test`, 4 vCPU, 8 GiB RAM).
The PBS-X test server was VM 104; no hypervisor reboot was required.

Run multiple live VSS test packages with `go test -p 1`. Parallel package
processes can request overlapping snapshots of the same volume and receive a
Windows shadow-copy provider veto. Ordinary unit tests can run in parallel.

## Results

- Full native host-agent `go test -count=1 -timeout=10m ./...`: passed.
- Affected Linux `agentfs`, `nativebackup` and PXAR package tests: passed
  natively on Linux using test executables cross-compiled by the Windows Go
  toolchain. This includes the mock Windows pipeline and parent confinement.
- Tagged `CGO_ENABLED=0` Windows agent build: passed.
- Opt-in real VSS test: point-in-time contents remain unchanged when the
  live file is modified; snapshot deletion succeeds.
- One-time enrollment, heartbeat, Windows service operation, queued backup,
  PBS snapshot commit, queued normal-mode restore and completion reporting:
  exercised through the actual agent and server APIs.
- Final backup timestamp: `1793510897`, datastore `backup`, group
  `host/pbsx-win-114`; 4 files and 2 directories, snapshot size 16,781,808 bytes.
- Restore destination: `C:\PBSX-E2E\restore\C-PBSX-E2E-source`.
- All four restored SHA-256 hashes match their sources, including the Unicode
  filename. The excluded subtree is absent, ADS payload matches, and the 16 MiB
  sparse file has matching allocated ranges.
- The protected file DACL, owner and group match the complete source SDDL;
  inheritance protection is retained.
- No shadow copies remained after the run. The agent service stayed running.

## Fixes found by the live run

1. An empty directory stream enumeration can terminate at `FindFirstStreamW`
   with `ERROR_HANDLE_EOF`; this is now treated as an empty stream list.
2. VSS paths cannot be resolved with `filepath.EvalSymlinks` through the
   `GLOBALROOT` namespace. Parent confinement uses Windows handles and NT final
   paths, retaining the check against symlink/reparse escapes.
3. Applying DACL metadata requires a handle with `READ_CONTROL` in addition to
   `WRITE_DAC|WRITE_OWNER`. Owner, group and DACL failures are reported separately.
4. DACL inheritance protection needs explicit metadata. New Windows backups
   record `user.pbsx.dacl.protected` and normal restores apply the corresponding
   protection flag. Older archives without this key retain their prior behavior;
   protection cannot be reconstructed from missing metadata.

## Test state and security cleanup

The temporary automation token and all three of its ACL grants were removed
after validation, and its secret and consumed enrollment-code files were deleted.
The enrolled Windows agent retains its normal host-scoped credentials. The
manual-only job, host, snapshot, fixture and restore are retained for inspection.
The diagnostic service privilege restriction was removed; the service uses its
original LocalSystem configuration. A pre-test configuration backup remains
root-only on the PBS-X test server.

## Scope

This validates one controlled file-backup fixture, not every Windows workload.
Application-writer consistency, large production datasets, all ACL forms,
empty/null DACL distinctions, inherited permissions under different destination
parents, EFS, reparse variants, reboot recovery and image/bare-metal restore still
need dedicated coverage. Existing backups lack the new DACL-protection key.

## Expanded live cases, 2026-11-01

The opt-in `TestWindowsPBSRoundTripCases` suite uses actual enrolled credentials
and calls the production native backup and restore runners against the test
datastore. It adds coverage beyond the first queued-service round trip:

- Multiple sources with identical basenames and colliding shortened aliases.
- Empty files and directories, Unicode content/names, and exclusions with a
  different case from the real directory name.
- Real NTFS hardlink identity and relative symbolic-link targets.
- Paths longer than 260 characters.
- An exclusively locked source file, readable through its VSS snapshot.
- A 240,000-byte ADS split across metadata chunks and a zero-length ADS.
- Read-only/hidden attributes, ADS on a read-only file, and modification time.
- A metadata-mode incremental snapshot after changing ADS data without changing
  its size or the file timestamp, adding a file, and deleting another file.
- A selected-file content-only restore.

All 11 subcases passed. Successful full and incremental snapshots were
`1793511939` and `1793511943` under `host/pbsx-win-114`. Each source fixture and
restore directory was removed by test cleanup. Snapshots remain for inspection.
`TestWindowsPBSRejectsOversizedADS` also passed: a 32 MiB + 1 byte ADS causes an
explicit backup failure with the size-limit message. No new administrator token
or ACL grant was needed for this pass.

Two production defects were found and fixed: the restore attribute decoder now
accepts the native backup's `readOnly`/`hidden` names as well as legacy Windows
constant names; and `FILE_BASIC_INFO` now has its required 40-byte size and 8-byte
alignment. Attributes and timestamps are applied after ADS writes so read-only
state cannot prevent stream creation and stream writes cannot alter the final
timestamp.
