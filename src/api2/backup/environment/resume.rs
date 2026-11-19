//! Opt-in continuation state. Partial indexes are ordinary, non-hidden DIDX
//! files so the existing GC marks their chunks. No snapshot manifest is written
//! by a checkpoint. Ordinary PBS sessions do not enable or retain this state.

use super::*;
use std::fs::{File, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::OpenOptionsExt;
use std::path::Path;

use pbs_datastore::dynamic_index::DynamicIndexReader;
use pbs_datastore::index::IndexFile;
use serde::Deserialize;

const RECORD: &str = ".pbsx-resume.json";
const NAMES: [&str; 2] = ["root.mpxar.didx", "root.ppxar.didx"];
const MAX_CHUNK: u64 = 16 << 20;

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct ArchivePrefix {
    name: String,
    prefix: String,
    size: u64,
    count: usize,
    checksum: [u8; 32],
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct ResumeRecord {
    version: u32,
    auth: String,
    token_hash: [u8; 32],
    sequence: u64,
    state_name: String,
    state_checksum: [u8; 32],
    archives: Vec<ArchivePrefix>,
}

pub(super) struct ResumeSession {
    token_hash: [u8; 32],
    record: Option<ResumeRecord>,
    pending_state: Option<(u64, [u8; 32])>,
}

fn token_hash(token: &str) -> Result<[u8; 32], Error> {
    if token.len() != 64
        || !token
            .bytes()
            .all(|b| b.is_ascii_hexdigit() && !b.is_ascii_uppercase())
    {
        bail!("resume requires a 256-bit lowercase hexadecimal token");
    }
    Ok(openssl::sha::sha256(token.as_bytes()))
}

fn slot(sequence: u64) -> &'static str {
    if sequence % 2 == 0 { "b" } else { "a" }
}

fn state_name(sequence: u64) -> String {
    format!("pbsx-resume-state-{}.blob", slot(sequence))
}

fn prefix_name(index: usize, sequence: u64) -> String {
    format!(
        "pbsx-resume-{}-{}.didx",
        if index == 0 { "metadata" } else { "payload" },
        slot(sequence)
    )
}

fn read_record(root: &Path, auth: &Authid, token: &str) -> Result<ResumeRecord, Error> {
    if root.join(MANIFEST_BLOB_NAME.as_ref()).exists() {
        bail!("cannot resume a completed snapshot");
    }
    let mut raw = Vec::new();
    File::open(root.join(RECORD))?
        .take(65537)
        .read_to_end(&mut raw)?;
    if raw.len() > 65536 {
        bail!("resume record exceeds size limit");
    }
    let record: ResumeRecord = serde_json::from_slice(&raw)?;
    let supplied = token_hash(token)?;
    if record.version != 1
        || record.auth != auth.to_string()
        || !openssl::memcmp::eq(&record.token_hash, &supplied)
        || record.sequence == 0
        || record.archives.len() != 2
        || record.state_name != state_name(record.sequence)
    {
        bail!("resume record does not belong to this authenticated client and token");
    }
    for (i, archive) in record.archives.iter().enumerate() {
        if archive.name != NAMES[i]
            || archive.prefix != prefix_name(i, record.sequence)
            || archive.size > i64::MAX as u64
            || archive.count > 1_600_000
        {
            bail!("invalid resume archive prefix");
        }
    }
    Ok(record)
}

fn persist_record(root: &Path, record: &ResumeRecord) -> Result<(), Error> {
    let temporary = root.join(format!(
        ".pbsx-resume-{}.tmp",
        proxmox_uuid::Uuid::generate()
    ));
    let result = (|| {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(&temporary)?;
        file.write_all(&serde_json::to_vec(record)?)?;
        file.sync_all()?;
        drop(file);
        std::fs::rename(&temporary, root.join(RECORD))?;
        File::open(root)?.sync_all()?;
        Ok(())
    })();
    if result.is_err() {
        let _ = std::fs::remove_file(&temporary);
    }
    result
}

fn checked_prefix(root: &Path, prefix: &ArchivePrefix) -> Result<DynamicIndexReader, Error> {
    let index = DynamicIndexReader::open(&root.join(&prefix.prefix))?;
    let (checksum, size) = index.compute_csum();
    if size != prefix.size
        || index.index_count() != prefix.count
        || checksum != prefix.checksum
        || checksum != index.index_csum
    {
        bail!("resume prefix checksum, length or count does not match");
    }
    let mut offset = 0;
    for i in 0..index.index_count() {
        let end = index.chunk_end(i);
        if end <= offset || end - offset > MAX_CHUNK {
            bail!("invalid resume chunk length");
        }
        offset = end;
    }
    Ok(index)
}

fn state_checksum(root: &Path, name: &str) -> Result<[u8; 32], Error> {
    let mut input = File::open(root.join(name))?;
    let mut hash = openssl::sha::Sha256::new();
    let mut buffer = [0u8; 65536];
    let mut total = 0usize;
    loop {
        let count = input.read(&mut buffer)?;
        if count == 0 {
            break;
        }
        total += count;
        if total > 64 << 20 {
            bail!("resume state exceeds size limit");
        }
        hash.update(&buffer[..count]);
    }
    Ok(hash.finish())
}

fn checked_state(root: &Path, record: &ResumeRecord) -> Result<(), Error> {
    if state_checksum(root, &record.state_name)? != record.state_checksum {
        bail!("resume state is missing, oversized or corrupt");
    }
    Ok(())
}

impl BackupEnvironment {
    /// Call before starting a worker for an existing directory. Failed token
    /// authorization must never enter the worker's failure-cleanup path.
    pub fn authorize_resume(root: &Path, auth: &Authid, token: &str) -> Result<(), Error> {
        read_record(root, auth, token)?;
        Ok(())
    }

    pub fn validate_resume_token(token: &str) -> Result<(), Error> {
        token_hash(token)?;
        Ok(())
    }

    pub fn configure_resume(&self, token: &str, existing: bool) -> Result<(), Error> {
        if !matches!(self.backend, DatastoreBackend::Filesystem) {
            bail!("resumable backup currently requires a filesystem datastore");
        }
        let root = self.backup_dir.full_path();
        let record = if existing {
            Some(read_record(&root, &self.auth_id, token)?)
        } else {
            None
        };
        let mut state = self.state.lock().unwrap();
        state.ensure_unfinished()?;
        if state.resume.is_some() {
            bail!("resume is already configured");
        }
        if let Some(record) = &record {
            checked_state(&root, record)?;
            for prefix in &record.archives {
                let index = checked_prefix(&root, prefix)?;
                let name = BackupArchiveName::try_from_strict(&prefix.name)?;
                let mut writer = self
                    .datastore
                    .create_dynamic_writer(self.backup_dir.relative_path().join(&prefix.name))?;
                let mut previous = 0;
                for i in 0..index.index_count() {
                    let end = index.chunk_end(i);
                    let digest = *index.index_digest(i).unwrap();
                    // Re-register only references held by this authenticated
                    // checkpoint, and check they still exist before reuse.
                    if !self.datastore.cond_touch_chunk(&digest, true)? {
                        bail!("resume chunk is missing");
                    }
                    state.known_chunks.insert(digest, (end - previous) as u32);
                    writer.add_chunk(end, &digest)?;
                    previous = end;
                }
                let wid = state.next_uid();
                state.dynamic_writers.insert(
                    wid,
                    DynamicWriterState {
                        name,
                        index: writer,
                        offset: prefix.size,
                        chunk_count: prefix.count as u64,
                        upload_stat: UploadStatistic::new(),
                        closed: false,
                    },
                );
            }
        }
        state.resume = Some(ResumeSession {
            token_hash: token_hash(token)?,
            record,
            pending_state: None,
        });
        Ok(())
    }

    /// Small JSON status; opaque encrypted continuation bytes and raw indexes
    /// are downloaded separately to retain the normal API body-size limits.
    pub fn resume_status(&self) -> Result<Value, Error> {
        let state = self.state.lock().unwrap();
        state.ensure_unfinished()?;
        let resume = state
            .resume
            .as_ref()
            .ok_or_else(|| format_err!("resume is not enabled"))?;
        let Some(record) = &resume.record else {
            return Ok(json!({"sequence": 0, "streams": []}));
        };
        let mut streams = Vec::new();
        for prefix in &record.archives {
            let (wid, _) = state
                .dynamic_writers
                .iter()
                .find(|(_, w)| w.name.as_ref() == prefix.name)
                .ok_or_else(|| format_err!("resume writer has already been closed"))?;
            streams.push(json!({"wid": wid, "name": prefix.name, "size": prefix.size,
                "count": prefix.count, "checksum": hex::encode(prefix.checksum)}));
        }
        Ok(
            json!({"sequence": record.sequence, "state_name": record.state_name,
            "state_checksum": hex::encode(record.state_checksum), "streams": streams}),
        )
    }

    pub fn resume_download_path(&self, name: &str) -> Result<std::path::PathBuf, Error> {
        let state = self.state.lock().unwrap();
        state.ensure_unfinished()?;
        let record = state
            .resume
            .as_ref()
            .and_then(|r| r.record.as_ref())
            .ok_or_else(|| format_err!("no resume checkpoint exists"))?;
        if name == "state" {
            checked_state(&self.backup_dir.full_path(), record)?;
            return Ok(self.backup_dir.full_path().join(&record.state_name));
        }
        let prefix = record
            .archives
            .iter()
            .find(|p| p.name == name)
            .ok_or_else(|| format_err!("unknown resume archive"))?;
        let _ = checked_prefix(&self.backup_dir.full_path(), prefix)?;
        let path = self.backup_dir.full_path().join(&prefix.prefix);
        if std::fs::metadata(&path)?.len() > 64 << 20 {
            bail!("resume index exceeds size limit");
        }
        Ok(path)
    }

    pub fn checkpoint_resume(&self, sequence: u64, expected: &Value) -> Result<Value, Error> {
        let mut state = self.state.lock().unwrap();
        state.ensure_unfinished()?;
        let resume = state
            .resume
            .as_ref()
            .ok_or_else(|| format_err!("resume is not enabled"))?;
        let previous = resume.record.as_ref().map(|r| r.sequence).unwrap_or(0);
        if sequence == 0 || previous.checked_add(1) != Some(sequence) {
            bail!("resume checkpoint sequence mismatch; reconnect before retrying");
        }
        if !state.fixed_writers.is_empty() || state.dynamic_writers.len() != 2 {
            bail!("resume checkpoint requires exactly the metadata and payload writers");
        }
        let root = self.backup_dir.full_path();
        let blob_name = state_name(sequence);
        let (uploaded_sequence, encrypted_checksum) = resume.pending_state.ok_or_else(|| {
            format_err!("encrypted continuation state has not been uploaded for this checkpoint")
        })?;
        if uploaded_sequence != sequence || state_checksum(&root, &blob_name)? != encrypted_checksum
        {
            bail!("continuation state does not match this checkpoint sequence");
        }
        let mut record = ResumeRecord {
            version: 1,
            auth: self.auth_id.to_string(),
            token_hash: resume.token_hash,
            sequence,
            state_name: blob_name,
            state_checksum: encrypted_checksum,
            archives: Vec::new(),
        };
        let expected = expected
            .as_array()
            .ok_or_else(|| format_err!("expected stream prefixes must be an array"))?;
        if expected.len() != 2 {
            bail!("expected exactly two stream prefixes");
        }
        for (i, name) in NAMES.iter().enumerate() {
            let writer = state
                .dynamic_writers
                .values_mut()
                .find(|w| w.name.as_ref() == *name)
                .ok_or_else(|| format_err!("resume stream {name} is missing"))?;
            let wanted = expected
                .iter()
                .find(|e| e["name"].as_str() == Some(name))
                .ok_or_else(|| format_err!("expected stream {name} is missing"))?;
            let filename = prefix_name(i, sequence);
            let (checksum, size, count) = writer.index.checkpoint(&root.join(&filename))?;
            if wanted["size"].as_u64() != Some(size)
                || wanted["count"].as_u64() != Some(count as u64)
                || wanted["checksum"].as_str() != Some(hex::encode(checksum).as_str())
                || size != writer.offset
                || count as u64 != writer.chunk_count
            {
                bail!("acknowledged stream differs from requested checkpoint");
            }
            record.archives.push(ArchivePrefix {
                name: name.to_string(),
                prefix: filename,
                size,
                count,
                checksum,
            });
        }
        persist_record(&root, &record)?;
        state.resume.as_mut().unwrap().record = Some(record);
        state.resume.as_mut().unwrap().pending_state = None;
        drop(state);
        self.resume_status()
    }

    /// A dedicated endpoint keeps opaque continuation data out of ordinary
    /// uploaded-archive/manifest accounting and protects the committed slot.
    pub fn upload_resume_state(&self, sequence: u64, data: &[u8]) -> Result<Value, Error> {
        if data.len() < 32 || data.len() > 16 << 20 {
            bail!("invalid encrypted continuation size");
        }
        let mut state = self.state.lock().unwrap();
        state.ensure_unfinished()?;
        let resume = state
            .resume
            .as_mut()
            .ok_or_else(|| format_err!("resume is not enabled"))?;
        if resume
            .record
            .as_ref()
            .map(|r| r.sequence)
            .unwrap_or(0)
            .checked_add(1)
            != Some(sequence)
        {
            bail!("continuation upload sequence mismatch");
        }
        let root = self.backup_dir.full_path();
        let temporary = root.join(format!(
            ".pbsx-resume-state-{}.tmp",
            proxmox_uuid::Uuid::generate()
        ));
        let result: Result<(), Error> = (|| {
            let mut file = OpenOptions::new()
                .write(true)
                .create_new(true)
                .mode(0o600)
                .open(&temporary)?;
            file.write_all(data)?;
            file.sync_all()?;
            drop(file);
            std::fs::rename(&temporary, root.join(state_name(sequence)))?;
            File::open(&root)?.sync_all()?;
            Ok(())
        })();
        if result.is_err() {
            let _ = std::fs::remove_file(&temporary);
        }
        result?;
        let checksum = openssl::sha::sha256(data);
        resume.pending_state = Some((sequence, checksum));
        Ok(json!({"checksum":hex::encode(checksum)}))
    }

    pub fn retain_resume_checkpoint(&self) -> bool {
        let state = self.state.lock().unwrap();
        // A failed final manifest/fsync must retain the ordinary failure
        // cleanup behavior. In particular, never preserve an uncommitted
        // manifest that would make an unfinished run look completed.
        state.finished == BackupState::Active
            && state
                .resume
                .as_ref()
                .and_then(|r| r.record.as_ref())
                .is_some()
    }

    pub(super) fn validate_resume_manifest(&self, manifest: &BackupManifest) -> Result<(), Error> {
        if manifest.files().len() != 2
            || manifest
                .files()
                .iter()
                .any(|f| !NAMES.contains(&f.filename.as_ref()))
            || NAMES.iter().any(|name| {
                !manifest
                    .files()
                    .iter()
                    .any(|f| f.filename.as_ref() == *name)
            })
        {
            bail!("resumable document manifest must contain only the two root archives");
        }
        Ok(())
    }

    // Called while the snapshot lock is still held, after its manifest exists.
    // Auxiliary names are fixed, never taken from a caller or restored record.
    pub(super) fn cleanup_resume_files(&self) {
        let root = self.backup_dir.full_path();
        for sequence in [1, 2] {
            for name in [
                state_name(sequence),
                prefix_name(0, sequence),
                prefix_name(1, sequence),
            ] {
                if let Err(err) = std::fs::remove_file(root.join(name)) {
                    if err.kind() != std::io::ErrorKind::NotFound {
                        self.log(format!("resume auxiliary cleanup failed: {err}"));
                    }
                }
            }
        }
        if let Err(err) = std::fs::remove_file(root.join(RECORD)) {
            if err.kind() != std::io::ErrorKind::NotFound {
                self.log(format!("resume record cleanup failed: {err}"));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resume_record_is_bound_to_auth_and_token_and_cannot_name_arbitrary_files()
    -> Result<(), Error> {
        let root = tempfile::tempdir()?;
        let auth: Authid = "family@pbs!phone".parse()?;
        let token = "a".repeat(64);
        let mut record = ResumeRecord {
            version: 1,
            auth: auth.to_string(),
            token_hash: token_hash(&token)?,
            sequence: 1,
            state_name: state_name(1),
            state_checksum: [0; 32],
            archives: NAMES
                .iter()
                .enumerate()
                .map(|(i, n)| ArchivePrefix {
                    name: n.to_string(),
                    prefix: prefix_name(i, 1),
                    size: 0,
                    count: 0,
                    checksum: [0; 32],
                })
                .collect(),
        };
        persist_record(root.path(), &record)?;
        assert_eq!(read_record(root.path(), &auth, &token)?.sequence, 1);
        assert!(read_record(root.path(), &auth, &"b".repeat(64)).is_err());
        assert!(read_record(root.path(), &"family@pbs!other".parse()?, &token).is_err());
        record.archives[0].prefix = "../outside.didx".into();
        persist_record(root.path(), &record)?;
        assert!(read_record(root.path(), &auth, &token).is_err());
        Ok(())
    }
}
