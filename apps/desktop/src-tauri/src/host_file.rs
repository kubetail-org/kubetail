// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//! `host.json`, in the config folder (`Folders::config`) — the
//! persisted-settings source of truth; the webview keeps no copy. See
//! docs/adr/2026-08-09-host-json-settings.md and
//! docs/adr/2026-11-21-host-json-lives-under-the-kubetail-config-directory.md.
//!
//! Reaches the webview as `window.__KUBETAIL_HOST__`, set before any page
//! script by [`init_script`], and as the [`UPDATED_EVENT`] broadcast after
//! every write. Writes come through the `update_host_file` command, which also
//! runs [`update_script`] in every window so a reload reads the latest file.
//! A page that has just loaded asks for the file itself (`read_host_file`).
//!
//! Versioned JSON, all-`Option` fields, partial patches ([`HostFilePatch`]) —
//! a new setting is a new `Option` field, not a new command. Defensive reads
//! (missing/corrupt → defaults), atomic writes (temp + rename).

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use tauri::Manager;

use crate::error::{AppError, Result};
use crate::paths::{self, Folders};

/// Schema version stamped into the file on every write, for future migrations.
const CURRENT_SCHEMA_VERSION: u32 = 1;

/// Broadcast to every window after `host.json` changes, payload = merged
/// [`HostFile`]. The string is hand-mirrored in the webview
/// (`src/lib/host-file.ts`).
pub const UPDATED_EVENT: &str = "host-file-updated";

const FILE_NAME: &str = "host.json";

/// The `sessionStorage` key holding the latest file a window was sent. It
/// survives a reload, which reruns [`init_script`] with its build-time snapshot.
const SESSION_KEY: &str = "kubetail:host-file";

/// Where `host.json` is ([`manage`]).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Location {
    /// In the resolved config folder.
    pub path: PathBuf,
    /// Whether the config folder was itself a link at launch
    /// ([`Folders::config_linked`]).
    pub config_linked: bool,
}

/// Manages the [`Location`] of `host.json` in the config directory. `setup`
/// calls it before the first window, which reads the file. Without a location
/// the windows paint the defaults and a change fails.
pub fn manage<R: tauri::Runtime>(app: &tauri::AppHandle<R>, folders: &Folders) {
    match &folders.config {
        Some(dir) => {
            app.manage(Location {
                path: dir.join(FILE_NAME),
                config_linked: folders.config_linked,
            });
        }
        None => tracing::warn!("host.json has no location"),
    }
}

/// Where `host.json` is, as [`manage`] resolved it.
pub fn location<R: tauri::Runtime>(app: &tauri::AppHandle<R>) -> Result<Location> {
    app.try_state::<Location>()
        .map(|location| location.inner().clone())
        .ok_or_else(|| AppError::Io(std::io::Error::other("host.json has no location")))
}

/// The user's color-scheme choice; serialized lowercase to match the webview's
/// `ColorSchemePreference` strings (`theme.tsx`).
#[derive(Serialize, Deserialize, Clone, Copy, PartialEq, Eq, Debug)]
#[serde(rename_all = "lowercase")]
pub enum ColorSchemePreference {
    System,
    Light,
    Dark,
}

/// Parsed `host.json`. Settings must stay `Option` so absent fields (older
/// files, first run) default at the point of use rather than failing the parse.
#[derive(Serialize, Deserialize, Clone, PartialEq, Eq, Debug)]
#[serde(rename_all = "camelCase")]
pub struct HostFile {
    pub schema_version: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub color_scheme_preference: Option<ColorSchemePreference>,
}

impl Default for HostFile {
    fn default() -> Self {
        Self {
            schema_version: CURRENT_SCHEMA_VERSION,
            color_scheme_preference: None,
        }
    }
}

/// Partial update to [`HostFile`]: `Some` overwrites, `None` leaves untouched.
/// Argument shape of `update_host_file` (webview camelCase JSON).
#[derive(Deserialize, Clone, Default, Debug)]
#[serde(rename_all = "camelCase")]
pub struct HostFilePatch {
    pub color_scheme_preference: Option<ColorSchemePreference>,
}

/// Read `host.json`; missing or corrupt → defaults (a corrupt file must never
/// take the host down — the next write replaces it wholesale).
pub fn read(path: &Path) -> HostFile {
    std::fs::read_to_string(path)
        .ok()
        .and_then(|contents| serde_json::from_str(&contents).ok())
        .unwrap_or_default()
}

/// Merge `patch` onto the current contents, stamp [`CURRENT_SCHEMA_VERSION`],
/// persist atomically (unique temp + rename; concurrent windows can't tear the
/// file, last writer wins), and return the merged result. Creates the parent
/// dir on first run.
pub fn update(location: &Location, patch: HostFilePatch) -> Result<HostFile> {
    let path = &location.path;
    let mut file = read(path);
    if let Some(preference) = patch.color_scheme_preference {
        file.color_scheme_preference = Some(preference);
    }
    file.schema_version = CURRENT_SCHEMA_VERSION;

    let parent = path
        .parent()
        .ok_or_else(|| std::io::Error::other("host file path has no parent directory"))?;
    // Owner-only again here, not just at launch: the directory may have been
    // removed since.
    paths::ensure_config_dir(parent, location.config_linked)?;

    let contents = serde_json::to_string_pretty(&file)?;

    // Unique temp name so concurrent writers never share a partial file; the
    // rename is atomic.
    static WRITE_COUNTER: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    let names = std::iter::repeat_with(|| {
        path.with_extension(format!(
            "tmp-{}-{}",
            std::process::id(),
            WRITE_COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
        ))
    });
    let tmp = write_new_file(names, contents.as_bytes())?;
    std::fs::rename(&tmp, path)?;

    Ok(file)
}

/// Writes `contents` to the first of `names` that does not exist yet, readable
/// by its owner alone, and returns it. A name already taken is passed over,
/// never opened: it would keep its own mode, or lead through a link. Windows
/// has no mode bits: there the profile's ACL covers the file.
fn write_new_file(
    names: impl IntoIterator<Item = PathBuf>,
    contents: &[u8],
) -> std::io::Result<PathBuf> {
    use std::io::Write;

    let mut options = std::fs::OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    std::os::unix::fs::OpenOptionsExt::mode(&mut options, 0o600);
    for name in names {
        match options.open(&name) {
            Ok(mut file) => {
                file.write_all(contents)?;
                return Ok(name);
            }
            Err(err) if err.kind() == std::io::ErrorKind::AlreadyExists => continue,
            Err(err) => return Err(err),
        }
    }
    Err(std::io::Error::other("no free temporary name"))
}

/// Initialization script exposing the file as `window.__KUBETAIL_HOST__`.
/// `build_window` injects it into every window; Tauri runs it before any page
/// script on every load, so the webview's first-paint code reads it
/// synchronously. `file` is the window's build-time snapshot: a load after a
/// write takes the file [`update_script`] stored instead, unless what is
/// stored is not a JSON object.
pub fn init_script(file: &HostFile) -> String {
    format!(
        "window.__KUBETAIL_HOST__ = (function () {{ \
         try {{ var latest = JSON.parse(sessionStorage.getItem(\"{SESSION_KEY}\")); \
         if (latest && typeof latest === \"object\" && !Array.isArray(latest)) return latest; \
         }} catch (e) {{}} \
         return {}; }})();",
        to_json(file)
    )
}

/// Script `window_manager::send_host_file` runs in a window: sets
/// `window.__KUBETAIL_HOST__` to `file` and stores it where [`init_script`]
/// looks on the next load.
pub fn update_script(file: &HostFile) -> String {
    format!(
        "window.__KUBETAIL_HOST__ = {}; \
         try {{ sessionStorage.setItem(\"{SESSION_KEY}\", \
         JSON.stringify(window.__KUBETAIL_HOST__)); }} catch (e) {{}}",
        to_json(file)
    )
}

fn to_json(file: &HostFile) -> String {
    // Fallback rather than unwrap; the inline script defaults missing fields.
    serde_json::to_string(file).unwrap_or_else(|_| "{}".into())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::testutil::{folders, roots, Scratch};
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicU64, Ordering};

    /// A location at `path` in a config folder that was no link.
    fn at(path: &Path) -> Location {
        Location {
            path: path.to_path_buf(),
            config_linked: false,
        }
    }

    /// A unique path inside the OS temp dir, in a directory that does not yet
    /// exist (exercising `update`'s create-parent-dirs behavior).
    fn temp_path() -> PathBuf {
        static COUNTER: AtomicU64 = AtomicU64::new(0);
        let id = COUNTER.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir()
            .join(format!(
                "kubetail-host-file-test-{}-{id}",
                std::process::id()
            ))
            .join("host.json")
    }

    #[test]
    fn read_missing_file_returns_defaults() {
        let file = read(&temp_path());
        assert_eq!(file, HostFile::default());
        assert_eq!(file.schema_version, CURRENT_SCHEMA_VERSION);
        assert_eq!(file.color_scheme_preference, None);
    }

    #[test]
    fn read_parses_existing_file() {
        let path = temp_path();
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(
            &path,
            r#"{"schemaVersion":1,"colorSchemePreference":"dark"}"#,
        )
        .unwrap();
        let file = read(&path);
        assert_eq!(
            file.color_scheme_preference,
            Some(ColorSchemePreference::Dark)
        );
    }

    #[test]
    fn read_corrupt_file_returns_defaults() {
        let path = temp_path();
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, "{not json").unwrap();
        assert_eq!(read(&path), HostFile::default());
    }

    #[test]
    fn update_writes_and_reloads() {
        let path = temp_path();
        // Parent dir deliberately absent — update must create it (first run).
        let updated = update(
            &at(&path),
            HostFilePatch {
                color_scheme_preference: Some(ColorSchemePreference::Dark),
            },
        )
        .unwrap();
        assert_eq!(
            updated.color_scheme_preference,
            Some(ColorSchemePreference::Dark)
        );
        // A fresh read round-trips the persisted value.
        assert_eq!(read(&path), updated);
    }

    #[test]
    fn update_with_empty_patch_preserves_existing_values() {
        let path = temp_path();
        update(
            &at(&path),
            HostFilePatch {
                color_scheme_preference: Some(ColorSchemePreference::Light),
            },
        )
        .unwrap();
        let updated = update(&at(&path), HostFilePatch::default()).unwrap();
        assert_eq!(
            updated.color_scheme_preference,
            Some(ColorSchemePreference::Light)
        );
    }

    #[test]
    fn update_stamps_current_version() {
        let path = temp_path();
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(
            &path,
            r#"{"schemaVersion":0,"colorSchemePreference":"light"}"#,
        )
        .unwrap();
        let updated = update(&at(&path), HostFilePatch::default()).unwrap();
        assert_eq!(updated.schema_version, CURRENT_SCHEMA_VERSION);
        assert_eq!(read(&path).schema_version, CURRENT_SCHEMA_VERSION);
    }

    /// The config directory is made owner-only at launch; a write after it was
    /// removed makes it so again, and the file is the owner's alone. Unix only
    /// — Windows has no POSIX mode bits.
    #[cfg(unix)]
    #[test]
    fn a_write_after_the_directory_went_is_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = Scratch::new();
        let app = tauri::test::mock_app();
        manage(app.handle(), &folders(scratch.path()));
        let location = location(app.handle()).unwrap();
        let path = location.path.clone();
        std::fs::remove_dir_all(path.parent().unwrap()).unwrap();

        update(&location, HostFilePatch::default()).unwrap();

        let mode = |p: &Path| std::fs::metadata(p).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode(path.parent().unwrap()), 0o700);
        assert_eq!(mode(&path), 0o600);
    }

    /// A temp name already taken, even by a link, is passed over, so the
    /// settings file never takes another file's mode or becomes a link.
    #[cfg(unix)]
    #[test]
    fn a_taken_temp_name_is_passed_over() {
        let scratch = Scratch::new();
        let target = scratch.path().join("target");
        std::fs::write(&target, "untouched").unwrap();
        let taken = scratch.path().join("taken");
        std::os::unix::fs::symlink(&target, &taken).unwrap();
        let free = scratch.path().join("free");

        let written = write_new_file([taken, free.clone()], b"new").unwrap();

        assert_eq!(written, free);
        assert_eq!(std::fs::read_to_string(&free).unwrap(), "new");
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "untouched");
    }

    /// Only a taken name is passed over; any other failure ends the write.
    #[test]
    fn write_new_file_stops_at_another_failure() {
        let scratch = Scratch::new();
        let unwritable = scratch.path().join("missing").join("tmp");
        let free = scratch.path().join("free");
        assert!(write_new_file([unwritable, free.clone()], b"new").is_err());
        assert!(!free.exists());
    }

    #[test]
    fn write_new_file_fails_when_every_name_is_taken() {
        let scratch = Scratch::new();
        let taken = scratch.path().join("taken");
        std::fs::write(&taken, "").unwrap();
        assert!(write_new_file([taken], b"new").is_err());
    }

    #[test]
    fn manage_sets_what_location_reads() {
        let scratch = Scratch::new();
        let app = tauri::test::mock_app();
        assert!(location(app.handle()).is_err());
        let folders = folders(scratch.path());

        manage(app.handle(), &folders);

        let expected = folders.config.unwrap().join("host.json");
        assert_eq!(location(app.handle()).unwrap().path, expected);
    }

    #[test]
    fn manage_without_a_config_dir_sets_no_location() {
        let scratch = Scratch::new();
        let roots = roots(scratch.path());
        let config = paths::current_layout(&roots).config.unwrap();
        std::fs::create_dir_all(config.parent().unwrap()).unwrap();
        std::fs::write(&config, "not a directory").unwrap();
        let app = tauri::test::mock_app();

        manage(app.handle(), &Folders::make(&roots).unwrap());

        assert!(location(app.handle()).is_err());
    }

    #[test]
    fn patch_deserializes_webview_camel_case_json() {
        let patch: HostFilePatch = serde_json::from_str(r#"{"colorSchemePreference":"system"}"#)
            .expect("patch should parse");
        assert_eq!(
            patch.color_scheme_preference,
            Some(ColorSchemePreference::System)
        );
    }

    #[test]
    fn init_script_exposes_the_file_as_a_global() {
        let file = HostFile {
            schema_version: 1,
            color_scheme_preference: Some(ColorSchemePreference::Dark),
        };
        assert_eq!(
            init_script(&file),
            "window.__KUBETAIL_HOST__ = (function () { \
             try { var latest = JSON.parse(sessionStorage.getItem(\"kubetail:host-file\")); \
             if (latest && typeof latest === \"object\" && !Array.isArray(latest)) return latest; \
             } catch (e) {} \
             return {\"schemaVersion\":1,\"colorSchemePreference\":\"dark\"}; })();"
        );
    }

    #[test]
    fn init_script_omits_absent_fields() {
        // An unset preference must be absent (webview reads `undefined`), not
        // `null` — the inline script treats both as "system", but absence keeps
        // the contract identical to the file on disk.
        assert!(init_script(&HostFile::default()).ends_with(r#"return {"schemaVersion":1}; })();"#));
    }

    /// The stored copy is `kubetail:*` storage: anything but a JSON object
    /// falls through to the build-time snapshot.
    #[test]
    fn init_script_takes_only_a_stored_object() {
        let script = init_script(&HostFile::default());
        assert!(
            script.contains(
                r#"if (latest && typeof latest === "object" && !Array.isArray(latest)) return latest;"#
            ),
            "{script}"
        );
    }

    /// A write after the window was built reaches its next load: the update
    /// stores the new file under the key the init script reads first.
    #[test]
    fn a_reload_after_a_write_reads_the_written_file() {
        let built = HostFile::default();
        let written = HostFile {
            color_scheme_preference: Some(ColorSchemePreference::Dark),
            ..HostFile::default()
        };

        let update = update_script(&written);
        assert!(
            update.starts_with(
                r#"window.__KUBETAIL_HOST__ = {"schemaVersion":1,"colorSchemePreference":"dark"};"#
            ),
            "{update}"
        );
        let reload = init_script(&built);
        let stored = format!(r#"sessionStorage.setItem("{SESSION_KEY}""#);
        let read = format!(r#"sessionStorage.getItem("{SESSION_KEY}")"#);
        assert!(update.contains(&stored), "{update}");
        assert!(reload.contains(&read), "{reload}");
    }
}
