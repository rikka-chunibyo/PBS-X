# Android device exports, format 1

Android 0.4.0 adds optional, independent Shizuku-backed exports. File sources and
normal SD-card folder selection remain separate from these provider readers.
All categories default to disabled. App selection affects APK/external app file
sources, not these device-wide categories for the current app profile.

## Archive layout

The PBS root archive contains `Device-data/<category>.zip`. Each ZIP contains
`manifest.json`, identifying `pbsx-android-provider-export`, format version 1,
the category and Android SDK. Records use UTF-8 JSON Lines, one object per line.
Strings, newlines and Unicode are JSON-escaped; numbers retain their numeric
representation, null stays null, and blobs are objects with `encoding: base64`
and `value`. Consumers should retain integer precision when decoding IDs and
timestamps. Vendor-specific columns are preserved rather than mapped to a
lossy fixed schema. Record IDs and account references belong to the source
profile and are not ready-to-insert destination IDs.

| Category | Contents |
| --- | --- |
| `contacts` | `contacts.jsonl`, `raw_contacts.jsonl`, `data.jsonl`, `groups.jsonl`, `contacts.vcf` and available full-size photos under `photos/<contact-id>.bin` |
| `sms` | `sms.jsonl` |
| `mms` | `messages.jsonl`, `addresses.jsonl`, `parts.jsonl`, available provider-backed attachments under `attachments/<part-id>.bin` |
| `calls` | `calls.jsonl` |
| `calendar` | `calendars.jsonl`, `events.jsonl`, `attendees.jsonl`, `reminders.jsonl`, `extendedproperties.jsonl` |
| `preferences` | `preferences.jsonl`, including explicit unset entries where applicable |

MMS address rows contain `message_id` and a `values` object. Parts retain their
provider columns except the private `_data` filesystem path. `pbsx_attachment`
names the corresponding ZIP entry when a provider file exists.
`pbsx_payload_state` distinguishes `provider-file`, `inline-text` and
`not-present`; a message whose attachment has not been downloaded cannot export
bytes absent from the provider. A failure to read an existing attachment fails
the export rather than claiming it was backed up. MIME types and original
filenames remain metadata only; attachment paths use validated numeric IDs.
Photo `.bin` files preserve the provider's encoding without assuming JPEG.

The preferences allowlist is limited to these `system` keys:
`font_scale`, `screen_brightness`, `screen_brightness_mode`,
`accelerometer_rotation`, `user_rotation`, `screen_off_timeout`,
`sound_effects_enabled`, `haptic_feedback_enabled` and `dtmf_tone`.
No broad secure-settings dump or account/credential export is performed.

## Reading and consistency

A Shizuku UserService runs the reader under shell identity. Its Binder surface
accepts only known category names; it does not expose arbitrary commands, URIs,
SQL or provider mutations. The provider adapter uses the read-only subset of
Android's content-command interfaces, with shell attribution and an explicitly
scoped user/profile. Android 13 is exercised in the emulator; unfamiliar hidden
API signatures on other versions fail visibly.

Exports stream across reliable pipes into app-private cache, using bounded
buffers and cancellation-aware polling. Partial streams, denied reads and
invalid/incomplete ZIPs are rejected before the PBS backup starts. The current
bounds are 512 MiB combined staged exports, 250,000 processed provider records
per category (including auxiliary passes), 512 columns, 2 MiB per text/blob
field and 4 MiB per serialized record. A category has a ten-minute timeout and
staging reserves 32 MiB of available storage. Reaching a bound fails the run;
it does not truncate the export. Ordinary SAF/Shizuku file sources keep their
existing streaming path and do not use this export cache.

Successful, failed and cancelled operations remove their temporary cache.
Process-start cleanup removes only this app's orphaned export cache after a
process death. No cache is placed on shared storage. ZIP entry timestamps are
fixed and no current export timestamp is embedded, allowing unchanged
deterministic provider output to reuse PBS chunks. Source providers may change
while being read; there is no multi-provider transaction, app force-stop or
system snapshot.

## Restore scope

The existing restore browser restores these ZIPs as ordinary files, in any
supported file conflict mode. A contacts application can manually import the
included standard vCard. PBS-X does not automatically install APKs, write app
folders through Shizuku, import provider records, modify system preferences or
recreate accounts. A future importer needs schema-aware ID/account mapping,
duplicate handling and independent restore tests. RCS and private messenger
databases are outside the SMS/MMS provider export.

Secure Folder is separate future work: run the app within that profile, with
its own credentials/access context, and add encryption before enabling that
workflow. Access from the main profile is not the planned approach.

## Test separation and references

Production export code contains only real provider readers. Debug-only fixture
classes route the same reader to synthetic records behind a shell-only DUMP
permission. They never proxy the real contacts/messages providers. Release APK
assembly excludes those fixture classes and providers. The real Samsung phone
was not updated, granted permissions, written to or used for data exports during
development of this version.

- [Shizuku UserService privileges and Context limitations](https://github.com/RikkaApps/Shizuku-API#userservice)
- [AOSP Android 13 content command](https://github.com/aosp-mirror/platform_frameworks_base/blob/android-13.0.0_r1/cmds/content/src/com/android/commands/content/Content.java)
- [Android contacts vCard URI](https://developer.android.com/reference/android/provider/ContactsContract.Contacts#CONTENT_VCARD_URI)
