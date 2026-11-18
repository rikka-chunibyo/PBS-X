# Keys, encryption and complete settings exports

Android preview 0.5.0 adds **Settings → Keys & credentials**. Connections and
backup encryption keys are separate named entries. Each job selects a connection
and either **Encryption: Off** or a saved encryption key. Job cards show both.
Jobs store entry IDs, so renaming a key/connection preserves the association.
Entries referenced by jobs cannot be removed. Removing local credentials does
not revoke a PBS API token; removing an encryption key does not alter snapshots.
Keep recovery copies of keys needed by historical snapshots.

Enrollment adds a saved connection automatically. Existing API keys can also
be added. A connection's optional storage override retains its enrolled device
identity. The default connection applies to Restore and new jobs; existing jobs
retain their selected connection. The Restore catalogue can select another
connection, and per-job Snapshots uses that job's connection. PBS still enforces
ordinary storage credentials, ACLs and ownership.

## Backup encryption

**Create encryption key** offers a random key or a **custom passphrase**.
The exact same custom passphrase recreates the same PBS key after a phone reset,
on another device, or with another server/account. No export, device secret or
random salt is needed for that recovery. The passphrase itself is never saved;
the derived 256-bit key is stored in the vault. Names do not affect derivation.
Spaces, capitalization and exact UTF-8 bytes matter; input is not trimmed or
normalized. Use a long, unique passphrase; derivation cannot add entropy to a
predictable phrase.

The permanent v1 recovery contract is scrypt with N=65536, r=8, p=1, output
length 32 bytes, and the fixed public domain salt
`PBS-X backup encryption passphrase v1` (UTF-8). This fixed salt is deliberate:
recovery must depend only on the memorized phrase. A golden test vector,
independently calculated using Python's hashlib.scrypt, guards compatibility.
Future derivation schemes must retain v1 recovery unchanged. The 30-character
test phrase `fixture custom key 0123456789A` derives to
`462800aab5ba6f765e55ea99fae5591e9718dbe906c26c1fcf2c713079a51c9d`.

**Import encryption key** accepts an existing raw 64-character hexadecimal key;
a complete settings import also transfers keys. Random keys still require a
separately saved recovery copy. Passphrase recovery recreates the encryption
key; connect to PBS with valid storage credentials to access the snapshots.
Secrets are stored in the existing Android Keystore-backed encrypted vault.
The device's Keystore wrapping key remains local; portable backup keys and API
credentials are inside that vault and are included in explicit settings exports.
The default permits unattended scheduled use; biometric-per-operation locking
is not implemented in this preview.

Encryption uses the native PBS chunk format, AES-256-GCM, keyed chunk digests
and signed manifests. Encryption happens on the device before upload; TLS is
still used. Restore selects the matching saved key by manifest fingerprint,
verifies its signature, validates archive indexes and authenticates chunk data.
A missing/invalid selected backup key fails instead of disabling encryption.
Unchanged encrypted chunks are deduplicated. Different keys use different chunk
identities. Key rotation affects future snapshots; retain older keys to read
older snapshots. Locally saved jobs sharing a server/datastore/namespace/Backup
ID must select the same encryption setting.

PBS retains readable snapshot/group identities, sizes, times and job notes.
Enabling encryption does not rewrite older unencrypted snapshots. This release
exposes encryption in Android; desktop credential/key selection is separate work.

A shared writer fix removes the unsupported top-level `crypt-mode` field from
PBS manifests. Encryption mode belongs on each archive entry. PBS rewrites
manifests through its schema, which previously discarded that field and thereby
invalidated signatures. A protocol regression and bidirectional restores with
the standard Rust `proxmox-backup-client` verify compatibility.

## Complete export

**Export all settings**, at the bottom of **Settings**, asks
**Encrypt exported JSON?**:

- **Plain JSON** writes the complete configuration, including secrets.
- **Use password** asks for one password and encrypts the complete JSON. It
  neither creates a saved encryption-key entry nor saves the password.

The file picker chooses the destination. No plaintext export file is staged in
app cache. The following information is included:

| Section | Contents |
| --- | --- |
| `vault` | All connection profiles, default selection, API credentials, enrolled certificates/private identities, backup keys, fingerprints and names. |
| `preferences.settings` | Every saved general preference, including browsing Wi-Fi settings and saved status. |
| `preferences.jobs-v1` | Every job, source label/URI, Shizuku shared-folder label/path, Backup ID, connection/key reference, schedule, constraints, Shizuku/app selections, export categories, scheduler allocator and retained activity history. |
| `folder_permissions` | Descriptions of the current persisted folder grants, for reference. |
| Metadata | Format version, app version and export time. |

The plain format is `pbsx-android-settings`, version 1. Preference values carry
explicit types (`string`, `boolean`, `int`, `long`, `float`, `strings`) so round
trips preserve the types Android expects. Jobs and history retain their saved
JSON strings within those preference values. Current saved application state
uses exactly the vault, settings and jobs stores; all three are covered.
Temporary backup/export caches and transient screen/transfer state are excluded.

Password exports remain JSON envelopes with format `pbsx-settings-encrypted`,
version 1. They use scrypt (N=65536, r=8, p=1), a fresh 32-byte salt, AES-256-GCM
with a fresh 12-byte nonce, and authenticated format/version/algorithm context.
Salt, nonce and ciphertext are base64 JSON strings. No backup key or stored API
credential participates in settings-file encryption. KDF parameters and input
lengths are bounded before deriving a key.

## Import

**Import all settings** reads plain or encrypted JSON, asks for the export
password when needed, validates the complete configuration, and presents a
replacement review with job/connection/key counts. It replaces local settings
and keys; export the current configuration first if it must be retained.
Malformed, unsupported, wrong-password and tampered imports do not replace state.
Validation includes key fingerprints, connection settings, job references,
shared-history encryption policy, limits and scheduler identities. Known
preferences must have the types their Android readers expect; legacy folder
and schedule defaults are validated before they can trigger job migration.
Unknown preferences still round-trip with their explicit saved types.

History policy checks use the same HTTPS-origin normalization as the Go client:
host casing, a trailing slash and an explicit/default port 443 do not define
separate histories. Job editing, imports, connection/default changes and backup
startup enforce the same encryption setting for a shared history.

The new vault and pending preference replacement are committed together as an
encrypted journal. An interrupted preference write is completed on the next
vault load/startup. Imports and exports serialize against job/profile changes,
and import is blocked during a transfer. Persisted schedules are reconciled
with the imported jobs. Imported schedules remain paused behind a separate
hold flag; their saved intervals, Wi-Fi and charging choices are retained.
Resume them explicitly in **Settings** after reviewing permissions.

Android controls folder grants, Shizuku authorization and notification access;
JSON cannot grant these on another device. Re-select unavailable folders using
the job editor and grant the required permissions on the destination. Interrupted
activity entries are recovered as interrupted, without claiming a completed run.

Limits are 16 MiB of plain settings JSON, 32 MiB input for encrypted envelopes,
100 jobs, 100 connections, 100 encryption keys and the app's existing 100-entry
activity history. Export/password fields are not saved in Android instance state.
The keys screen and settings-export password dialogs also disable screenshots
using Android's secure window flag.

This is a testing-format change. Clear 0.4.x app storage and reconnect before
using 0.5.0; no old-vault migration is provided. The user's phone is not modified
by development/testing. App settings are **not** added as a backup source.

## Shizuku defaults

New jobs start with Shizuku disabled, all source/export checkboxes unchecked,
All apps unchecked and no selected packages. While Shizuku is disabled, its
source checkboxes, app controls and export choices are disabled and dimmed.
Toggling the master switch preserves the job's choices. Existing saved jobs
retain their explicit selections.
