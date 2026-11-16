# Desktop and release readiness work

Branch: `pbsx-agent-desktop-readiness`.

The requested scope includes a **desktop application window** for Windows and
Linux, with Linux retaining a complete headless CLI. The desktop is a companion
to the privileged background agent; closing a window must not stop scheduled
backups. GUI and CLI should share operations and avoid duplicating backup logic.

## Acceptance milestones

1. Agent releases and updates: PBS-X version policy, authenticated release
   artifacts, reproducible Windows/Linux packaging, safe replacement and rollback,
   and preservation of enrollment/storage credentials. Updates remain opt-in.
2. Desktop and CLI: native application window, enrollment, service/connection
   state and live operation progress. Administrative actions go through the
   privileged agent; the GUI must not copy its credential vault into user settings.
   Linux CLI remains usable without graphics libraries or a running desktop.
3. Server maintenance gate: stop new dispatch/claims while preparing an update,
   wait for active work, and clear maintenance safely after success or failure.
4. Host restore browser: browse authorized archives, select paths and queue a
   restore while retaining datastore/namespace/group identity and ACL checks.
5. Automated release checks: portable/race tests, platform compilation, native
   GUI checks, package/manifest validation and publication gates.
6. Failure tests: full disks, denied destinations, interrupted transfers,
   cancellation, interrupted updates and cleanup/retry behavior.

Hardware-dependent and physical-device acceptance remain separate. Build and
mock/temporary-filesystem checks must report their scope explicitly.

## Findings at the starting point

- Native entry points and updater defaults still use the inherited
  `>= 0.52.0` range; the PBS-X preview is `0.6.0-preview`.
- A server `embedded` flag bypasses update signatures.
- Main entry points commit a pending update after a timer even if enrollment or
  server communication has not succeeded. Healthy commit should follow an
  authenticated successful heartbeat instead.
- The existing updater uses the compatibility agent port/API; the production
  PBS-X release channel is not yet wired. Automatic updates are off by default.
- Server maintenance checks currently have a check/stop race.
- The Host Backups view supports entering restore paths but has no file browser.
- Desktop CLI currently emphasizes enrollment/service control and internal child
  process modes; user-facing status and operations need a shared command model.

## Implemented in this branch

- Native Fyne window for Windows/Linux: dark theme, enrollment, connection and
  service state, live operation counters/activity bar, and start/stop/restart.
  The companion closes independently of the background service. A separate
  static `pbsx-agentctl` provides headless Linux administration and status.
- The GUI requests elevation only for changes. Enrollment input travels in a
  private temporary file, not process arguments or saved preferences. The
  service publishes a separate limited public status snapshot. Windows vault
  keys now have explicit SYSTEM/Administrators-only protected ACLs, including
  retained legacy keys; machine-DPAPI encryption alone was insufficient.
- Matched-version bundle builder, Linux/systemd and Windows preview installers,
  corruption/architecture checks and executable/service rollback on ordinary
  install failures. Existing installation paths, credentials and service
  configuration are retained. No public client feed is published.
- Updater minimum version corrected for PBS-X previews; signatures required
  regardless of `embedded`; version rollback rejected; update checks defer to
  active native work. Pending updates commit after authenticated heartbeat,
  and CLI commands no longer count as failed service boots. Both initial and
  periodic checks wait for bootstrap and honor cancellation.
- Native host queue/claim mutations hold a shared maintenance lock. APT holds
  the exclusive lock across the idle check, stop and archive; the existing
  transaction state blocks native dispatch through package-triggered restarts.
  The next protection package is **1.0.3**, built locally only; the paired
  server changelog is **4.2.9-1+pbsx2 (UNRELEASED)**. This requires
  the corresponding server code; it does not gate every upstream PBS/PVE
  writer/reader. Public APT packages and services were not upgraded by this work.
- Host Backups gains server-authorized file browsing, ordinary downloads, and
  selection of a file/directory into the restore form. Namespace and backup ID
  survive browsing and cross-host restore selection. Split and legacy root
  archives, Unicode decoding, unsafe path rejection and encrypted-archive
  refusal are covered by UI tests. Real-browser acceptance remains pending.
- Read-only GitHub Actions workflow for Linux races, native Windows unit tests,
  desktop builds, installer checks, UI tests and Linux virtual-display smoke.
  It uploads previews without publishing/signing credentials.

## Validation, 2026-11-16

- Full Linux agent race suite and native Windows unit suite passed.
- Linux native install/reinstall, ordinary-user public status, and deliberate
  failed-update executable/service rollback passed in VM103. The temporary
  agent was removed afterward; original cached Rust release sources restored.
- Windows install, service start/stop, public status, credential preservation
  and deliberate failed-update rollback passed in VM114. The earlier lab
  source-tree service was explicitly moved to the protected Program Files path.
- Native Windows vault ACL and legacy-import confinement tests passed. The
  protected ACL permits exactly SYSTEM and Administrators. Public status uses
  its separate ordinary-reader key.
- Real CLI enrollment through the privileged setup helper against VM115 passed,
  including automatic namespace/key creation, service installation and an
  authenticated heartbeat. One-time enrollment input was not printed.
- Native Linux desktop rendered under Xvfb/software OpenGL. Windows demo process
  started and stayed running in the SSH session; there was no interactive
  console session, so Windows visual/UAC acceptance remains pending.
- Rust/Python flock interoperability and transaction-marker test passed; all
  14 `pbs-config` unit tests passed; the server library passed `cargo check`.
- Eleven APT hook tests, ten bundle/source-archive safety tests and both Host UI scripts
  passed. `pbsx-repository_1.0.3_all.deb` built locally; not published.

Hosted CI also passed for source commit `986617a06`: portable source export,
Linux race/UI/installer/render checks, and native Windows unit tests, desktop
build and bundle validation. [Successful run](https://github.com/rikka-chunibyo/PBS-X/actions/runs/43943105476).
The portable source export produced byte-identical static Linux agent/CLI
executables compared with the Git-checkout build. All three lab VMs were returned
to their original stopped state; VM103's 32 GiB/8 GiB balloon settings were restored.
The dev VM and hypervisor were left running.

## Remaining release work

1. Interactive Windows/Linux UAC/PolicyKit enrollment checks and real-browser
   restore acceptance; native full-job and hardware soak tests on final packages.
2. Production signed client release channel and manifest/version binding,
   installer publisher authentication (including Windows Authenticode), and
   interrupted-power recovery drills. Automatic updates remain off by default.
3. Full local job creation, credential selection and restore management in the
   desktop. This first companion uses the server UI for those operations.
4. Wider backup failure/large-data coverage and gating of upstream PBS workers
   during maintenance. The new gate specifically covers native host dispatch.
5. Release the paired server and protection-package update after acceptance. This branch is a preview,
   not a production release or an APT publication.
