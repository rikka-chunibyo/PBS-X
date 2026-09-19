# PBS-X agent

This directory contains the first cross-platform PBS-X agent control-plane client. It intentionally does **not** contain backup logic yet. Its job in the foundation phase is to prove the host identity, authentication, TLS pinning, heartbeat and basic inventory path on both Linux and Windows without introducing third-party runtime dependencies.

The agent uses an ordinary PBS API token. The token should be granted the `Host.Agent` role only on the matching host path, for example `/host/desktop-01`.

A minimal manual enrollment flow for the foundation build is:

```sh
proxmox-backup-manager user create pbsx-agent@pbs
proxmox-backup-manager user generate-token pbsx-agent@pbs desktop-01
proxmox-backup-manager acl update /host/desktop-01 HostAgent --auth-id 'pbsx-agent@pbs!desktop-01'
```

Save the generated token secret in `PBSX_TOKEN_SECRET`. Automatic one-time enrollment and credential rotation are intentionally left for the next agent pass; the server-side permission boundary is already in place.

```sh
export PBSX_SERVER='https://pbs.example:8007'
export PBSX_HOST_ID='desktop-01'
export PBSX_TOKEN_ID='pbsx-agent@pbs!desktop-01'
export PBSX_TOKEN_SECRET='...'
export PBSX_FINGERPRINT='AA:BB:...'

./pbsx-agent --once
```

Without `--once`, the agent sends a heartbeat every 60 seconds. Heartbeats currently include OS/architecture data plus disk and mounted-filesystem inventory. Linux inventory is read directly from `/sys`, `/proc` and `statfs`; Windows inventory uses the built-in PowerShell/CIM interfaces. The server records the timestamp itself and stores runtime state separately from `host.cfg`.

The next agent work should add persistent configuration/service installation, automatic enrollment/token rotation, richer inventory/capability reporting, and then the Linux/Windows backup providers described in `docs/pbsx-roadmap.md`.
