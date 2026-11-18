//! Durable completion transitions shared by host backups and restores.
//! Callers hold the host's orchestration lock throughout these operations.

use std::path::Path;

use anyhow::{Context, Error, bail};
use serde::{Serialize, de::DeserializeOwned};

fn read_optional<T: DeserializeOwned>(path: &Path) -> Result<Option<T>, Error> {
    let raw = match std::fs::read(path) {
        Ok(raw) => raw,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(err) => {
            return Err(err).with_context(|| format!("read run state '{}'", path.display()));
        }
    };
    serde_json::from_slice(&raw)
        .with_context(|| format!("parse run state '{}'", path.display()))
        .map(Some)
}

fn retire_matching<C: DeserializeOwned + Serialize>(
    running: &Path,
    command: &C,
) -> Result<(), Error> {
    if let Some(current) = read_optional::<C>(running)? {
        if serde_json::to_value(current)? != serde_json::to_value(command)? {
            bail!("running command conflicts with completed history");
        }
        std::fs::remove_file(running)?;
        if let Some(parent) = running.parent() {
            std::fs::File::open(parent)?.sync_all()?;
        }
    }
    Ok(())
}

/// Persist one immutable completion before retiring the claimed command.
/// A retry validates the existing record and never rewrites its result/time.
pub(super) fn finish<C, H>(
    running: &Path,
    history: &Path,
    make: impl FnOnce(C) -> Result<H, Error>,
    validate: impl Fn(&H) -> Result<(), Error>,
    command: impl Fn(&H) -> &C,
    write: impl FnOnce(&Path, &H) -> Result<(), Error>,
) -> Result<(), Error>
where
    C: DeserializeOwned + Serialize,
    H: DeserializeOwned,
{
    let record = match read_optional::<H>(history)? {
        Some(record) => record,
        None => {
            let Some(command) = read_optional::<C>(running)? else {
                bail!("run is not currently claimed and has no completion history");
            };
            let record = make(command)?;
            validate(&record)?;
            write(history, &record)?;
            record
        }
    };
    validate(&record)?;
    retire_matching(running, command(&record))
}

/// Recover an interrupted completion before handing work to an agent again.
pub(super) fn retire_completed<C, H>(
    running: &Path,
    history: &Path,
    validate: impl Fn(&H) -> Result<(), Error>,
    command: impl Fn(&H) -> &C,
) -> Result<bool, Error>
where
    C: DeserializeOwned + Serialize,
    H: DeserializeOwned,
{
    let Some(record) = read_optional::<H>(history)? else {
        return Ok(false);
    };
    validate(&record)?;
    retire_matching(running, command(&record))?;
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::{Deserialize, Serialize};
    use std::{
        cell::Cell,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    #[derive(Clone, Debug, Deserialize, Serialize)]
    struct Command {
        host: String,
        run: String,
        source: String,
    }
    #[derive(Clone, Debug, PartialEq, Deserialize, Serialize)]
    struct ResultInfo {
        host: String,
        run: String,
        success: bool,
    }
    #[derive(Clone, Debug, Deserialize, Serialize)]
    struct History {
        command: Command,
        result: ResultInfo,
        finished: u64,
    }

    struct Fixture {
        root: PathBuf,
        running: PathBuf,
        history: PathBuf,
        result: ResultInfo,
    }
    impl Fixture {
        fn new(kind: &str) -> Self {
            static NEXT: AtomicU64 = AtomicU64::new(0);
            let root = std::env::temp_dir().join(format!(
                "pbsx-completion-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            ));
            let running = root.join(format!("{kind}-running/run.json"));
            let history = root.join(format!("{kind}-history/run.json"));
            std::fs::create_dir_all(running.parent().unwrap()).unwrap();
            std::fs::create_dir_all(history.parent().unwrap()).unwrap();
            let result = ResultInfo {
                host: "fixture".into(),
                run: "run".into(),
                success: true,
            };
            let command = Command {
                host: result.host.clone(),
                run: result.run.clone(),
                source: "original".into(),
            };
            std::fs::write(&running, serde_json::to_vec(&command).unwrap()).unwrap();
            Self {
                root,
                running,
                history,
                result,
            }
        }
        fn validate(&self, history: &History) -> Result<(), Error> {
            if history.command.host != self.result.host
                || history.command.run != self.result.run
                || history.result != self.result
            {
                bail!("conflicting completion");
            }
            Ok(())
        }
        fn finish(&self, time: u64, writes: &Cell<usize>) -> Result<(), Error> {
            finish(
                &self.running,
                &self.history,
                |command| {
                    Ok(History {
                        command,
                        result: self.result.clone(),
                        finished: time,
                    })
                },
                |history| self.validate(history),
                |history| &history.command,
                |path, record| {
                    writes.set(writes.get() + 1);
                    std::fs::write(path, serde_json::to_vec(record)?)?;
                    Ok(())
                },
            )
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.root);
        }
    }

    #[test]
    fn lost_ack_retry_preserves_completion_and_allows_next_claim() {
        for kind in ["backup", "restore"] {
            let fixture = Fixture::new(kind);
            let writes = Cell::new(0);
            fixture.finish(10, &writes).unwrap();
            let original = std::fs::read(&fixture.history).unwrap();
            fixture.finish(20, &writes).unwrap();
            assert_eq!(writes.get(), 1);
            assert_eq!(std::fs::read(&fixture.history).unwrap(), original);
            assert!(!fixture.running.exists());
            // A dispatcher sees an empty running queue after restart/retry.
            assert_eq!(
                std::fs::read_dir(fixture.running.parent().unwrap())
                    .unwrap()
                    .count(),
                0
            );
        }
    }

    #[test]
    fn conflicting_retry_never_rewrites_history() {
        for kind in ["backup", "restore"] {
            let mut fixture = Fixture::new(kind);
            let writes = Cell::new(0);
            fixture.finish(10, &writes).unwrap();
            let original = std::fs::read(&fixture.history).unwrap();
            fixture.result.success = false;
            assert!(fixture.finish(20, &writes).is_err());
            assert_eq!(writes.get(), 1);
            assert_eq!(std::fs::read(&fixture.history).unwrap(), original);
        }
    }

    #[test]
    fn interrupted_finish_is_retired_without_rerunning_job() {
        for kind in ["backup", "restore"] {
            let fixture = Fixture::new(kind);
            let command =
                serde_json::from_slice(&std::fs::read(&fixture.running).unwrap()).unwrap();
            let record = History {
                command,
                result: fixture.result.clone(),
                finished: 10,
            };
            std::fs::write(&fixture.history, serde_json::to_vec(&record).unwrap()).unwrap();
            assert!(
                retire_completed(
                    &fixture.running,
                    &fixture.history,
                    |h| fixture.validate(h),
                    |h| &h.command
                )
                .unwrap()
            );
            assert!(!fixture.running.exists());
            let writes = Cell::new(0);
            fixture.finish(20, &writes).unwrap();
            assert_eq!(writes.get(), 0);
        }
    }

    #[test]
    fn failed_history_write_keeps_running_command() {
        let fixture = Fixture::new("backup");
        let result = finish(
            &fixture.running,
            &fixture.history,
            |command| {
                Ok(History {
                    command,
                    result: fixture.result.clone(),
                    finished: 10,
                })
            },
            |history| fixture.validate(history),
            |history| &history.command,
            |_, _| bail!("injected write failure"),
        );
        assert!(result.is_err());
        assert!(fixture.running.exists());
        assert!(!fixture.history.exists());
    }

    #[test]
    fn malformed_history_or_different_command_is_not_discarded() {
        let fixture = Fixture::new("restore");
        std::fs::write(&fixture.history, b"incomplete").unwrap();
        assert!(fixture.finish(10, &Cell::new(0)).is_err());
        assert!(fixture.running.exists());
        let command = Command {
            host: fixture.result.host.clone(),
            run: fixture.result.run.clone(),
            source: "different".into(),
        };
        let record = History {
            command,
            result: fixture.result.clone(),
            finished: 10,
        };
        std::fs::write(&fixture.history, serde_json::to_vec(&record).unwrap()).unwrap();
        assert!(
            retire_completed(
                &fixture.running,
                &fixture.history,
                |h| fixture.validate(h),
                |h| &h.command
            )
            .is_err()
        );
        assert!(fixture.running.exists());
    }
}
