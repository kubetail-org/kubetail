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

//! Where the app keeps its files on disk.
//!
//! One place resolves the leaf ([`app_dir_name`]), because every directory
//! the app keeps needs it, and a second copy of the join is what silently drops
//! the `-dev` split.

use std::ffi::OsString;
use std::path::{Component, Path, PathBuf};

use tauri::{AppHandle, Manager, Runtime};

use crate::error::{AppError, Result};

/// The platform a layout is for. Each platform's [`layout`] is a pure function
/// of it, so every platform's paths are tested on every OS.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
#[cfg_attr(test, derive(serde::Deserialize), serde(rename_all = "lowercase"))]
enum Platform {
    Linux,
    MacOs,
    Windows,
}

impl Platform {
    /// The platform this build runs on.
    const CURRENT: Self = if cfg!(target_os = "linux") {
        Self::Linux
    } else if cfg!(target_os = "macos") {
        Self::MacOs
    } else {
        Self::Windows
    };
}

/// The leaf each of Kubetail's directories takes under its platform root. A
/// dev build uses a `-dev` sibling so a dev run never collides with an
/// installed release. Linux names folders in lowercase; macOS and Windows show
/// the product name.
const fn app_dir_name(platform: Platform, dev: bool) -> &'static str {
    match (platform, dev) {
        (Platform::Linux, true) => "kubetail-dev",
        (Platform::Linux, false) => "kubetail",
        (Platform::MacOs | Platform::Windows, true) => "Kubetail-dev",
        (Platform::MacOs | Platform::Windows, false) => "Kubetail",
    }
}

/// Where each kind of directory sits on one platform, whether or not it
/// could be made.
#[derive(Debug, PartialEq, Eq)]
#[cfg_attr(test, derive(serde::Deserialize), serde(deny_unknown_fields))]
pub(crate) struct Layout {
    /// The folder macOS and Windows keep several kinds in. `None` on Linux,
    /// where each kind has a root. A per-user Windows install puts the
    /// release's executable in it too, so it is never deleted whole.
    pub(crate) app: Option<PathBuf>,
    /// Windows' app folder on the config root, which holds the config alone.
    /// It is `app` when the config root is the local data root.
    pub(crate) app_on_config_root: Option<PathBuf>,
    pub(crate) data: PathBuf,
    /// `None` when the platform names no config root.
    pub(crate) config: Option<PathBuf>,
    pub(crate) state: PathBuf,
    pub(crate) cache: PathBuf,
    pub(crate) logs: PathBuf,
    /// Windows' alone: Unix keeps it under the session's runtime root
    /// ([`runtime_dir`]).
    #[cfg_attr(unix, allow(dead_code))]
    pub(crate) runtime: Option<PathBuf>,
}

/// Each kind takes a folder of its own, and none holds another's, so the
/// sidecar fences each and `make clean-desktop-data` deletes each by its path.
///
/// - Linux gives every kind a root (XDG's), so each is a leaf on its root.
/// - macOS has a root for the cache (`~/Library/Caches`, which backups skip)
///   and the logs (`~/Library/Logs`, which Console.app lists). The data and
///   config roots are one folder and there is no state root, so those three
///   are subfolders of one app folder.
/// - Windows' cache root is its local data root, and it has no state or
///   runtime root, so those kinds are siblings in one app folder under
///   `%LOCALAPPDATA%`, whose ACL covers them. The logs sit in the state, as on
///   Linux. The config roams, in `%APPDATA%`, as a subfolder of the app
///   folder on that root: were that root the local data root, the config
///   would still be a sibling of the other kinds.
fn layout_for(platform: Platform, dev: bool, roots: &Roots) -> Layout {
    let leaf = app_dir_name(platform, dev);
    let app = roots.local_data.join(leaf);
    match platform {
        Platform::Linux => {
            let state = roots.state.join(leaf);
            Layout {
                app: None,
                app_on_config_root: None,
                data: app,
                config: roots.config.as_ref().map(|dir| dir.join(leaf)),
                logs: state.join("logs"),
                state,
                cache: roots.cache.join(leaf),
                runtime: None,
            }
        }
        Platform::MacOs => Layout {
            data: app.join("data"),
            // In the app folder whatever the config root is, which decides
            // only whether there is a config directory.
            config: roots.config.as_ref().map(|_| app.join("config")),
            state: app.join("state"),
            cache: roots.cache.join(leaf),
            logs: roots.home.join("Library/Logs").join(leaf),
            runtime: None,
            app_on_config_root: None,
            app: Some(app),
        },
        Platform::Windows => Layout {
            data: app.join("data"),
            config: roots
                .config
                .as_ref()
                .map(|dir| dir.join(leaf).join("config")),
            app_on_config_root: roots.config.as_ref().map(|dir| dir.join(leaf)),
            state: app.join("state"),
            cache: app.join("cache"),
            logs: app.join("state").join("logs"),
            runtime: Some(app.join("run")),
            app: Some(app),
        },
    }
}

/// `platform`'s layout for this build's profile.
fn layout(platform: Platform, roots: &Roots) -> Layout {
    layout_for(platform, cfg!(debug_assertions), roots)
}

/// This platform's layout.
pub(crate) fn current_layout(roots: &Roots) -> Layout {
    layout(Platform::CURRENT, roots)
}

/// The platform directories Kubetail's own hang off, resolved once from the
/// app. A test builds one over a scratch directory: Tauri's directory override
/// does not reach these. The sidecar reads the same roots from the
/// environment (`sidecar/internal/app/layout`).
#[cfg_attr(test, derive(serde::Deserialize), serde(deny_unknown_fields))]
pub struct Roots {
    /// `None` when the platform cannot name one: the preferences are then
    /// unavailable, never a failed launch.
    pub config: Option<PathBuf>,
    pub local_data: PathBuf,
    pub cache: PathBuf,
    pub home: PathBuf,
    /// XDG's state root, which only Linux reads.
    pub state: PathBuf,
}

impl Roots {
    pub fn of<R: Runtime>(app: &AppHandle<R>) -> Result<Self> {
        let path = app.path();
        let home = resolved(path.home_dir(), "home_dir")?;
        let state = state_home(std::env::var_os("XDG_STATE_HOME"), &home);
        Self::new(
            path.config_dir().ok(),
            resolved(path.local_data_dir(), "local_data_dir")?,
            resolved(path.cache_dir(), "cache_dir")?,
            home,
            state,
        )
    }

    /// The roots, each refused when it holds `..` ([`plain_root`]), which
    /// fails the launch: the sidecar fences the config, state and log
    /// directories by name, so the host's files could sit outside the fence.
    /// A root is checked where a directory hangs off it: the state root on
    /// Linux, the home on macOS, whose logs it holds.
    pub(crate) fn new(
        config: Option<PathBuf>,
        local_data: PathBuf,
        cache: PathBuf,
        home: PathBuf,
        state: PathBuf,
    ) -> Result<Self> {
        Ok(Self {
            config: config.map(plain_root).transpose()?,
            local_data: plain_root(local_data)?,
            cache: plain_root(cache)?,
            home: if cfg!(target_os = "macos") {
                plain_root(home)?
            } else {
                home
            },
            state: if cfg!(target_os = "linux") {
                plain_root(state)?
            } else {
                state
            },
        })
    }
}

/// XDG's state root: `$XDG_STATE_HOME` when it is absolute, which the spec
/// requires, else `~/.local/state`.
fn state_home(xdg_state_home: Option<OsString>, home: &Path) -> PathBuf {
    xdg_state_home
        .map(PathBuf::from)
        .filter(|dir| dir.is_absolute())
        .unwrap_or_else(|| home.join(".local/state"))
}

fn resolved(dir: tauri::Result<PathBuf>, name: &str) -> Result<PathBuf> {
    dir.map_err(|e| AppError::Io(std::io::Error::other(format!("resolve {name}: {e}"))))
}

/// `dir`, refused when it holds `..`. On Unix a `..` after a link names the
/// link target's parent, while the sidecar and `make clean-desktop-data` take it out
/// by name, so the host's files and their fences could be different folders.
/// The check reads the path alone, so it answers the same at every call.
/// Windows reads `..` by name itself.
fn plain_root(dir: PathBuf) -> Result<PathBuf> {
    if cfg!(unix) && dir.components().any(|c| c == Component::ParentDir) {
        return Err(AppError::Io(std::io::Error::other(format!(
            "{} holds `..`; set it without one",
            dir.display()
        ))));
    }
    Ok(dir)
}

/// The host's folders, every path with its links followed, made once at
/// launch ([`Folders::make`]). Nothing after it follows a link to one.
#[derive(Debug)]
pub struct Folders {
    /// What one machine keeps between runs and a user would not miss: the
    /// window frame and, on Linux and Windows, the logs and the webview's
    /// profile.
    pub state: PathBuf,
    /// `None` when the log folder cannot be made: the host logs to stderr.
    pub logs: Option<PathBuf>,
    /// What a user sets, which Windows roams with the profile. `None` when
    /// the platform names no config root or the folder cannot be made: the
    /// preferences are then unavailable.
    pub config: Option<PathBuf>,
    /// Whether the config folder was itself a link at launch, such as a
    /// dotfiles manager's: its target's mode is then the user's.
    pub config_linked: bool,
    /// The webview's profile, `<state>/webview`, which the webview makes.
    #[cfg(not(target_os = "macos"))]
    pub webview: PathBuf,
    /// As [`runtime_dir`] names it, unresolved: the sidecar resolves it.
    pub runtime: PathBuf,
}

impl Folders {
    /// Follows the links of every folder the layout names, checks them, and
    /// only then makes the host's own where they lead: the app folder above a
    /// kind first, the state before the logs it holds. A link to nothing, a
    /// linked `<state>/webview` and, on Windows, a link out of the profile
    /// fail the launch.
    pub fn make(roots: &Roots) -> Result<Self> {
        let named = current_layout(roots);
        let config_linked = named.config.as_deref().is_some_and(is_link);
        let layout = followed_layout(&named)?;
        if Platform::CURRENT == Platform::Windows {
            let profile = followed(&roots.home)?;
            check_in_profile(&named, &layout, &profile, through_a_link)?;
        }

        if let Err(err) = make_state(&layout) {
            // On macOS the state holds the window frame alone, which the app
            // can do without; elsewhere it holds the logs and the webview.
            if Platform::CURRENT != Platform::MacOs {
                return Err(err);
            }
            tracing::warn!(%err, "no state directory; the window frame is not kept");
        }
        let logs = make_logs(&layout)
            .inspect_err(|err| tracing::error!(%err, "no log directory; logging to stderr only"))
            .ok();
        let config = make_config(&layout, config_linked)
            .inspect_err(|err| tracing::warn!(%err, "no config directory"))
            .ok();
        Ok(Self {
            #[cfg(not(target_os = "macos"))]
            webview: webview_dir(&layout.state)?,
            state: layout.state,
            logs,
            config,
            config_linked,
            runtime: runtime_dir(roots)?,
        })
    }
}

impl Layout {
    /// Every folder the layout names. [`followed_layout`] lists the same
    /// fields.
    fn dirs(&self) -> impl Iterator<Item = &PathBuf> {
        [
            self.app.as_ref(),
            self.app_on_config_root.as_ref(),
            Some(&self.data),
            self.config.as_ref(),
            Some(&self.state),
            Some(&self.cache),
            Some(&self.logs),
            self.runtime.as_ref(),
        ]
        .into_iter()
        .flatten()
    }
}

/// `layout` with every folder's links followed ([`followed`]).
fn followed_layout(layout: &Layout) -> Result<Layout> {
    let follow = |dir: &Option<PathBuf>| dir.as_deref().map(followed).transpose();
    Ok(Layout {
        app: follow(&layout.app)?,
        app_on_config_root: follow(&layout.app_on_config_root)?,
        data: followed(&layout.data)?,
        config: follow(&layout.config)?,
        state: followed(&layout.state)?,
        cache: followed(&layout.cache)?,
        logs: followed(&layout.logs)?,
        runtime: follow(&layout.runtime)?,
    })
}

/// `path` with every link followed, as the kernel names it. A name that is
/// missing ends the walk, and the rest is kept as given; a link whose target
/// is missing is an error naming both. Windows' verbatim `\\?\` spelling is
/// never answered: a path only it can spell is an error.
fn followed(path: &Path) -> Result<PathBuf> {
    let mut at = path;
    let mut rest = Vec::new();
    loop {
        match dunce::canonicalize(at) {
            Ok(mut resolved) => {
                refuse_verbatim(&resolved)?;
                resolved.extend(rest.iter().rev());
                return Ok(resolved);
            }
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => {
                if is_link(at) {
                    let target = std::fs::read_link(at).map_err(AppError::Io)?;
                    return Err(AppError::Io(std::io::Error::other(format!(
                        "{} is a link to {}, which does not exist",
                        at.display(),
                        target.display()
                    ))));
                }
                // coverage:ignore-start the root is always there, so a missing path has a parent
                let (Some(parent), Some(name)) = (at.parent(), at.file_name()) else {
                    return Err(AppError::Io(err));
                };
                // coverage:ignore-stop
                rest.push(name);
                at = parent;
            }
            Err(err) => {
                return Err(AppError::Io(std::io::Error::other(format!(
                    "follow {}: {err}",
                    path.display()
                ))))
            }
        }
    }
}

/// Whether a link is on `path`: at it or at a folder above it.
fn through_a_link(path: &Path) -> bool {
    path.ancestors().any(is_link)
}

/// Whether `path` is itself a link: a symlink, or on Windows a junction.
fn is_link(path: &Path) -> bool {
    std::fs::symlink_metadata(path).is_ok_and(|meta| meta.file_type().is_symlink())
}

/// Refuses a resolved `path` still in Windows' verbatim spelling: one no
/// other spelling can name. No resolved Unix path starts that way.
fn refuse_verbatim(path: &Path) -> Result<()> {
    if path.to_string_lossy().starts_with(VERBATIM_PREFIX) {
        return Err(AppError::Io(std::io::Error::other(format!(
            "{} can be named only by a verbatim path; move it",
            path.display()
        ))));
    }
    Ok(())
}

const VERBATIM_PREFIX: &str = r"\\?\";

/// Refuses a layout folder a link leads out of the resolved `profile`, or
/// onto it: on Windows its ACL is what keeps Kubetail's files private. A folder
/// reached through no link (`linked`) is where Windows names it, which an
/// administrator may redirect off the profile, onto a mapped drive among
/// others. Paths are resolved, so their names compare exactly.
fn check_in_profile(
    named: &Layout,
    resolved: &Layout,
    profile: &Path,
    linked: impl Fn(&Path) -> bool,
) -> Result<()> {
    for (named, resolved) in named.dirs().zip(resolved.dirs()) {
        let inside = resolved
            .strip_prefix(profile)
            .is_ok_and(|below| below.components().next().is_some());
        if !inside && linked(named) {
            return Err(AppError::Io(std::io::Error::other(format!(
                "{} leads to {}, outside your profile; keep it inside {}",
                named.display(),
                resolved.display(),
                profile.display()
            ))));
        }
    }
    Ok(())
}

/// Makes the app folder owner-only when `dir` is in it, before `dir` is made
/// or read: whoever can write a folder can replace what is in it, so a private
/// kind in an open app folder is not private. One already owner-only is left
/// untouched, as [`ensure_config_dir`] leaves the config.
fn ensure_app_dir_above(layout: &Layout, dir: &Path) -> Result<()> {
    let Some(app) = layout.app.as_ref().filter(|app| dir.starts_with(app)) else {
        return Ok(());
    };
    if std::fs::metadata(app).is_ok_and(|meta| is_owner_only_dir(&meta)) {
        return Ok(());
    }
    ensure_data_dir(app)
}

/// The state folder, made owner-only, in an owner-only app folder.
fn make_state(layout: &Layout) -> Result<()> {
    ensure_app_dir_above(layout, &layout.state)?;
    ensure_data_dir(&layout.state)
}

/// The log folder, made owner-only. On Linux and Windows it sits in the state
/// folder, made just before it.
///
/// Not Tauri's `app_log_dir`: it names the leaf by bundle id rather than the
/// display name everything else here uses, and it has no debug variant, so a
/// dev run would write into an installed release's logs.
fn make_logs(layout: &Layout) -> Result<PathBuf> {
    ensure_app_dir_above(layout, &layout.logs)?;
    owner_only(layout.logs.clone())
}

/// The config folder, made as [`ensure_config_dir`] makes it.
fn make_config(layout: &Layout, linked: bool) -> Result<PathBuf> {
    let dir = layout
        .config
        .clone()
        .ok_or_else(|| AppError::Io(std::io::Error::other("resolve config_dir: none")))?;
    ensure_app_dir_above(layout, &dir)?;
    ensure_config_dir(&dir, linked)?;
    Ok(dir)
}

/// Makes the config folder at the resolved `path` owner-only, and tightens
/// an open one. One that was itself a link at launch (`linked`), or is one
/// now, keeps its target's mode: a dotfiles repository's mode is the user's. One already
/// owner-only is left untouched, so a read-only mount still yields its
/// `host.json`. One removed since launch is made again.
pub fn ensure_config_dir(path: &Path, linked: bool) -> Result<()> {
    let linked = linked || is_link(path);
    let keep = std::fs::metadata(path)
        .is_ok_and(|meta| meta.is_dir() && (linked || is_owner_only_dir(&meta)));
    if keep {
        return Ok(());
    }
    ensure_data_dir(path)
}

#[cfg(unix)]
fn is_owner_only_dir(meta: &std::fs::Metadata) -> bool {
    use std::os::unix::fs::PermissionsExt;

    meta.is_dir() && meta.permissions().mode() & 0o777 == 0o700
}

/// Windows has no mode bits: the profile's ACL is the gate.
#[cfg(not(unix))]
fn is_owner_only_dir(_meta: &std::fs::Metadata) -> bool {
    false
}

/// Creates `path` owner-only, and tightens one an earlier build already
/// created under the umask. On Windows the per-user `%LOCALAPPDATA%` ACL
/// already restricts it, so the plain create is enough.
pub fn ensure_data_dir(path: &Path) -> Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::{DirBuilderExt, PermissionsExt};

        std::fs::DirBuilder::new()
            .recursive(true)
            .mode(0o700)
            .create(path)
            .map_err(AppError::Io)?;
        // A mode on DirBuilder applies only to directories it creates.
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o700))
            .map_err(AppError::Io)?;
    }
    #[cfg(not(unix))]
    std::fs::create_dir_all(path).map_err(AppError::Io)?;
    Ok(())
}

/// `dir`, created owner-only ([`ensure_data_dir`]).
fn owner_only(dir: PathBuf) -> Result<PathBuf> {
    ensure_data_dir(&dir)?;
    Ok(dir)
}

/// The webview's profile directory on Linux and Windows, under the resolved
/// `state`: a leaf on the state directory, so a dev build keeps its own and
/// the sidecar's fence covers it. The webview makes the directory itself,
/// inside a state directory this user has made owner-only: whoever owns a
/// directory can replace what is in it. WebKitGTK takes the path as a string,
/// so on Linux one that is not UTF-8 is refused: its lossy spelling would be a
/// folder outside the fence. So is a link, a junction included, which would
/// lead the profile out of it.
#[cfg(not(target_os = "macos"))]
fn webview_dir(state: &Path) -> Result<PathBuf> {
    let dir = state.join("webview");
    if is_link(&dir) {
        return Err(AppError::Io(std::io::Error::other(format!(
            "{} is a link; link the state directory instead",
            dir.display()
        ))));
    }
    if cfg!(target_os = "linux") && dir.to_str().is_none() {
        return Err(AppError::Io(std::io::Error::other(format!(
            "{} is not UTF-8, which the webview's profile needs",
            dir.display()
        ))));
    }
    Ok(dir)
}

/// The directory for what lives for a session, owner-only: the IPC endpoint,
/// the shell snapshot, and each sandboxed run's kubeconfig and socket.
///
/// The gain is on Linux, where the fallback would be the shared, sticky-bit
/// `/tmp`: `$XDG_RUNTIME_DIR` is a per-user directory the session manager
/// already creates `0700`. On macOS `$TMPDIR` is per-user and `0700` too, so
/// there the subdirectory is tidiness rather than a fix.
///
/// A directory left behind by a crashed run is adopted, not refused. Nothing
/// sweeps it, because a second window of a running app shares it and a
/// concurrent copy owns its own. Outside `$XDG_RUNTIME_DIR` a name that is
/// refused gives way to one beside it ([`private_dir_or_fresh`]).
#[cfg(unix)]
fn runtime_dir(_roots: &Roots) -> Result<PathBuf> {
    // SAFETY: `geteuid` reads process state and cannot fail.
    let uid = unsafe { libc::geteuid() };
    let xdg = runtime_home(std::env::var_os("XDG_RUNTIME_DIR"));
    let dir = runtime_dir_path(xdg.as_deref(), uid)?;
    if xdg.is_none() {
        return private_dir_or_fresh(&dir, uid);
    }
    ensure_private_dir(&dir, uid)?;
    Ok(dir)
}

/// `dir`, made private by [`ensure_private_dir`], or when that refuses it a
/// directory beside it: the one an earlier launch made, else a fresh one. The
/// name under the temp dir is predictable, so another user can claim it first;
/// a random suffix cannot be claimed ahead. Launches keep to one directory
/// because the sidecar sweeps what a crashed run left in the one it is given:
/// a fresh one per launch would never be swept.
#[cfg(unix)]
fn private_dir_or_fresh(dir: &Path, uid: u32) -> Result<PathBuf> {
    match ensure_private_dir(dir, uid) {
        Ok(()) => Ok(dir.to_path_buf()),
        Err(err) => {
            tracing::warn!(%err, "runtime directory refused; using one beside it");
            match earlier_fresh_dir(dir, uid) {
                Some(earlier) => Ok(earlier),
                None => fresh_private_dir(dir),
            }
        }
    }
}

/// The first directory beside `dir` named as [`fresh_private_dir`] names one
/// that [`ensure_private_dir`] adopts: a directory this user owns, never a
/// link. Another user cannot make one this user owns, and the temp dir is
/// sticky, so none is swapped in after the check.
#[cfg(unix)]
fn earlier_fresh_dir(dir: &Path, uid: u32) -> Option<PathBuf> {
    let parent = dir.parent()?;
    let prefix = format!("{}-", dir.file_name()?.to_str()?);
    let mut names: Vec<String> = std::fs::read_dir(parent)
        .ok()?
        .filter_map(|entry| entry.ok()?.file_name().into_string().ok())
        .filter(|name| {
            name.strip_prefix(&prefix)
                .is_some_and(|suffix| suffix.len() == FRESH_SUFFIX_LEN)
        })
        .collect();
    names.sort();
    names
        .into_iter()
        .map(|name| parent.join(name))
        .find(|candidate| ensure_private_dir(candidate, uid).is_ok())
}

/// The length of the random suffix `mkdtemp` writes over `XXXXXX`.
#[cfg(unix)]
const FRESH_SUFFIX_LEN: usize = 6;

/// A new directory named `dir` plus a random suffix, made `0700` by `mkdtemp`,
/// which fails rather than reuse anything already there.
#[cfg(unix)]
fn fresh_private_dir(dir: &Path) -> Result<PathBuf> {
    use std::os::unix::ffi::{OsStrExt, OsStringExt};

    let mut template = dir.as_os_str().as_bytes().to_vec();
    template.extend_from_slice(b"-XXXXXX\0");
    // SAFETY: the template is NUL-terminated, and mkdtemp rewrites only its
    // trailing XXXXXX, in place.
    let made = unsafe { libc::mkdtemp(template.as_mut_ptr().cast()) };
    if made.is_null() {
        return Err(AppError::Io(std::io::Error::last_os_error()));
    }
    template.pop();
    Ok(PathBuf::from(std::ffi::OsString::from_vec(template)))
}

/// Creates `dir` `0700`, or adopts one an earlier run left behind — but only
/// once this user is proved to own it, and never through a symlink.
///
/// The order is the whole protection: tightening first would let another user
/// point `/tmp/kubetail-<uid>` at a directory of ours and have the app chmod it
/// on their behalf. Past the check there is no swap to lose to — `/tmp` is
/// sticky, so only the owner can replace the entry.
#[cfg(unix)]
fn ensure_private_dir(dir: &Path, uid: u32) -> Result<()> {
    use std::os::unix::fs::{DirBuilderExt, MetadataExt, PermissionsExt};

    match std::fs::DirBuilder::new().mode(0o700).create(dir) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {
            // A name is not ownership, and `symlink_metadata` is what refuses a
            // link: whoever planted it owns where it leads.
            let meta = std::fs::symlink_metadata(dir).map_err(AppError::Io)?;
            if !meta.is_dir() || meta.uid() != uid {
                return Err(AppError::Io(std::io::Error::other(format!(
                    "{} is not a directory owned by this user",
                    dir.display()
                ))));
            }
        }
        Err(e) => return Err(AppError::Io(e)),
    }
    // mkdir's mode is masked by the umask, and an adopted directory carries
    // whatever mode the earlier run left it with.
    std::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o700)).map_err(AppError::Io)
}

/// The directory for what lives for a session. Windows has no per-user runtime
/// directory, and `TEMP` can point at one users share, so it sits in the app
/// folder under `%LOCALAPPDATA%`, which the profile ACL covers ([`layout`]).
/// The named pipe ignores it: its namespace is flat, so the DACL and the peer
/// check are its whole policy.
#[cfg(not(unix))]
fn runtime_dir(roots: &Roots) -> Result<PathBuf> {
    let layout = current_layout(roots);
    let dir = layout
        .runtime
        .clone()
        .ok_or_else(|| AppError::Io(std::io::Error::other("no runtime directory")))?;
    ensure_app_dir_above(&layout, &dir)?;
    owner_only(dir)
}

/// XDG's runtime root: `$XDG_RUNTIME_DIR` when it is absolute, which the spec
/// requires, and a directory. A relative one would name a different folder
/// from each working directory.
#[cfg(unix)]
fn runtime_home(xdg_runtime_dir: Option<OsString>) -> Option<PathBuf> {
    xdg_runtime_dir
        .map(PathBuf::from)
        .filter(|dir| dir.is_absolute() && dir.is_dir())
}

/// Where [`runtime_dir`] puts the directory: under `$XDG_RUNTIME_DIR` when the
/// session manager gave us one, else under the temp dir.
///
/// The temp dir is shared between users on Linux, so the uid goes in the name:
/// one directory for all of them means the first user to run owns it `0700` and
/// every later user's chmod fails with EPERM.
#[cfg(unix)]
fn runtime_dir_path(xdg: Option<&Path>, uid: u32) -> Result<PathBuf> {
    Ok(match xdg {
        Some(base) => plain_root(base.to_path_buf())?.join(RUNTIME_DIR_NAME),
        None => plain_root(std::env::temp_dir())?.join(format!("{RUNTIME_DIR_NAME}-{uid}")),
    })
}

/// Leaf for [`runtime_dir`]. A `-dev` sibling like [`app_dir_name`]'s, so a dev
/// run and an installed release never share a socket directory.
#[cfg(unix)]
const RUNTIME_DIR_NAME: &str = if cfg!(debug_assertions) {
    "kubetail-dev"
} else {
    "kubetail"
};

#[cfg(test)]
mod tests {
    use super::*;

    /// The folders `Folders::make` makes over `roots`, which must not fail.
    fn made(roots: &Roots) -> Folders {
        Folders::make(roots).expect("make")
    }

    /// `dir`, made. Under a [`crate::testutil::Scratch`] it is as the kernel
    /// names it.
    fn made_dir(dir: &Path) -> PathBuf {
        std::fs::create_dir_all(dir).unwrap();
        dir.to_path_buf()
    }

    #[test]
    fn a_path_with_no_link_is_followed_to_itself() {
        let scratch = crate::testutil::Scratch::new();
        let dir = made_dir(&scratch.path().join("a"));

        assert_eq!(followed(&dir).unwrap(), dir);
        assert_eq!(
            followed(&dir.join("missing/kubetail")).unwrap(),
            dir.join("missing/kubetail"),
            "a missing tail is kept as given"
        );
    }

    #[cfg(unix)]
    #[test]
    fn a_linked_folder_is_followed_to_its_target() {
        let scratch = crate::testutil::Scratch::new();
        let target = made_dir(&scratch.path().join("big/kubetail"));
        let link = scratch.path().join("link");
        std::os::unix::fs::symlink(&target, &link).unwrap();

        assert_eq!(followed(&link).unwrap(), target);
        assert_eq!(followed(&link.join("logs")).unwrap(), target.join("logs"));
    }

    /// A path only a verbatim prefix can spell is refused by name.
    #[test]
    fn a_verbatim_path_is_refused() {
        let verbatim = Path::new(r"\\?\C:\Users\x\con");

        let err = refuse_verbatim(verbatim).unwrap_err().to_string();

        assert!(err.contains(&verbatim.display().to_string()), "got: {err}");
        assert!(refuse_verbatim(Path::new("/home/x")).is_ok());
        assert!(refuse_verbatim(Path::new(r"C:\Users\x")).is_ok());
    }

    /// A layout folder that is a link to nothing fails the launch, and the
    /// message names both ends.
    #[cfg(unix)]
    #[test]
    fn a_link_to_nothing_is_an_error_naming_both_ends() {
        let scratch = crate::testutil::Scratch::new();
        let link = scratch.path().join("link");
        let target = scratch.path().join("gone");
        std::os::unix::fs::symlink(&target, &link).unwrap();

        let err = followed(&link.join("kubetail")).unwrap_err().to_string();

        assert!(err.contains(&link.display().to_string()), "got: {err}");
        assert!(err.contains(&target.display().to_string()), "got: {err}");
    }

    /// A cycle of links stops the launch, naming the path.
    #[cfg(unix)]
    #[test]
    fn a_cycle_of_links_is_an_error() {
        let scratch = crate::testutil::Scratch::new();
        let (a, b) = (scratch.path().join("a"), scratch.path().join("b"));
        std::os::unix::fs::symlink(&b, &a).unwrap();
        std::os::unix::fs::symlink(&a, &b).unwrap();

        let err = followed(&a.join("kubetail")).unwrap_err().to_string();

        assert!(
            err.contains(&a.join("kubetail").display().to_string()),
            "got: {err}"
        );
    }

    /// The host's folders are the layout's, made, every path followed.
    #[test]
    fn folders_are_the_layouts_and_are_made() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let layout = current_layout(&roots);

        let folders = made(&roots);

        assert_eq!(folders.state, layout.state);
        assert_eq!(folders.logs.as_ref(), Some(&layout.logs));
        assert_eq!(folders.config, layout.config);
        assert!(folders.state.is_dir());
        assert!(layout.logs.is_dir());
        assert!(layout.config.unwrap().is_dir());
    }

    /// A linked state, log and config folder each launch, and the folders
    /// name the target.
    #[cfg(unix)]
    #[test]
    fn a_linked_layout_folder_is_named_by_its_target() {
        let scratch = crate::testutil::Scratch::new();
        let base = scratch.path();
        let roots = crate::testutil::roots(base);
        let layout = current_layout(&roots);
        let link = |dir: &Path, name: &str| {
            let target = made_dir(&base.join("elsewhere").join(name));
            std::fs::create_dir_all(dir.parent().unwrap()).unwrap();
            std::os::unix::fs::symlink(&target, dir).unwrap();
            target
        };
        let state = link(&layout.state, "state");
        let config = link(layout.config.as_ref().unwrap(), "config");

        let folders = made(&roots);

        assert_eq!(folders.state, state);
        assert_eq!(folders.config, Some(config));
        // On Linux the logs are in the state, and so where its link leads.
        assert_eq!(folders.logs, Some(followed(&layout.logs).unwrap()));
    }

    /// A log folder linked to nothing fails the launch, where one that cannot
    /// be made leaves the host on stderr.
    #[cfg(unix)]
    #[test]
    fn a_log_folder_linked_to_nothing_fails_the_launch() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let logs = current_layout(&roots).logs;
        std::fs::create_dir_all(logs.parent().unwrap()).unwrap();
        std::os::unix::fs::symlink(scratch.path().join("gone"), &logs).unwrap();

        assert!(Folders::make(&roots).is_err());
    }

    /// On Windows a link may not lead a layout folder out of the profile,
    /// whose ACL keeps it private; one Windows itself names outside it is
    /// the administrator's. Checked by text on resolved paths, so on every
    /// OS: `linked` says whether a link led each folder to `resolved`.
    #[test]
    fn a_link_may_not_lead_a_layout_folder_out_of_the_profile() {
        let scratch = crate::testutil::Scratch::new();
        let profile = made_dir(&scratch.path().join("profile"));
        let roots = |local: PathBuf| Roots {
            config: Some(local.clone()),
            local_data: local.clone(),
            cache: local,
            home: profile.clone(),
            state: profile.join("state"),
        };
        let on = |local: PathBuf| layout(Platform::Windows, &roots(local));
        let named = on(profile.join("AppData/Local"));
        assert!(check_in_profile(&named, &on(profile.join("moved")), &profile, |_| true).is_ok());

        let outside = on(scratch.path().join("D/big"));
        let err = check_in_profile(&named, &outside, &profile, |_| true)
            .unwrap_err()
            .to_string();
        assert!(err.contains("outside your profile"), "got: {err}");
        assert!(err.contains(&profile.display().to_string()), "got: {err}");
        assert!(
            check_in_profile(&outside, &outside, &profile, |_| false).is_ok(),
            "a folder Windows redirects"
        );

        let mut itself = on(profile.join("moved"));
        itself.app = Some(profile.clone());
        assert!(
            check_in_profile(&named, &itself, &profile, |_| true).is_err(),
            "the profile itself"
        );

        let sibling = on(profile.with_file_name("PROFILE").join("Local"));
        assert!(
            check_in_profile(&named, &sibling, &profile, |_| true).is_err(),
            "a sibling in another case"
        );
    }

    /// The data directory is the outer wall around the sidecar's caches, and the
    /// only one Windows has. Unix only — Windows has no POSIX mode bits.
    #[cfg(unix)]
    #[test]
    fn ensure_data_dir_creates_and_tightens_to_0700() {
        use std::os::unix::fs::PermissionsExt;

        let base = std::env::temp_dir().join(format!("kubetail-ensure-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);

        let fresh = base.join("fresh");
        ensure_data_dir(&fresh).expect("create");
        assert_eq!(
            std::fs::metadata(&fresh).unwrap().permissions().mode() & 0o777,
            0o700
        );

        // Every install before this landed has a 0755 directory, and a mode on
        // DirBuilder applies only to directories it creates.
        let existing = base.join("existing");
        std::fs::create_dir(&existing).unwrap();
        std::fs::set_permissions(&existing, std::fs::Permissions::from_mode(0o755)).unwrap();
        ensure_data_dir(&existing).expect("tighten");
        assert_eq!(
            std::fs::metadata(&existing).unwrap().permissions().mode() & 0o777,
            0o700
        );

        std::fs::remove_dir_all(&base).unwrap();
    }

    /// The log file holds cluster names and server hostnames, the same class of
    /// text as the data directory beside it, so the directory is the gate:
    /// files inside it are created under the umask. Unix only — Windows has no
    /// POSIX mode bits.
    #[cfg(unix)]
    #[test]
    fn log_dir_is_created_0700() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());

        let dir = made(&roots).logs.expect("create");
        assert_eq!(
            std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777,
            0o700
        );
    }

    /// Settings are the user's, like the data. Unix only — Windows has no POSIX
    /// mode bits.
    #[cfg(unix)]
    #[test]
    fn config_dir_is_created_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let dir = made(&crate::testutil::roots(scratch.path()))
            .config
            .expect("create");
        assert_eq!(
            std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777,
            0o700
        );
    }

    #[cfg(unix)]
    #[test]
    fn a_linked_config_dir_keeps_its_targets_mode() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let target = scratch.path().join("dotfiles");
        std::fs::create_dir(&target).unwrap();
        std::fs::set_permissions(&target, std::fs::Permissions::from_mode(0o755)).unwrap();
        let link = current_layout(&roots).config.unwrap();
        std::fs::create_dir_all(link.parent().unwrap()).unwrap();
        std::os::unix::fs::symlink(&target, &link).unwrap();

        let folders = made(&roots);
        assert_eq!(folders.config, Some(target.clone()));
        assert!(folders.config_linked);
        let location = crate::host_file::Location {
            path: target.join("host.json"),
            config_linked: folders.config_linked,
        };
        crate::host_file::update(&location, Default::default()).unwrap();
        assert!(
            target.join("host.json").is_file(),
            "host.json lands in the target"
        );
        assert_eq!(
            std::fs::metadata(&target).unwrap().permissions().mode() & 0o777,
            0o755
        );
    }

    /// A link above the config folder does not count: the folder below is
    /// one Kubetail makes, owner-only. Linux alone puts the config on its own
    /// root.
    #[cfg(target_os = "linux")]
    #[test]
    fn a_config_dir_under_a_linked_root_is_made_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let target = scratch.path().join("dotconfig");
        std::fs::create_dir(&target).unwrap();
        let root = scratch.path().join("config-link");
        std::os::unix::fs::symlink(&target, &root).unwrap();
        let roots = Roots {
            config: Some(root),
            ..crate::testutil::roots(scratch.path())
        };

        let folders = made(&roots);

        let config = folders.config.unwrap();
        assert!(config.starts_with(&target), "{}", config.display());
        assert!(!folders.config_linked);
        assert_eq!(
            std::fs::metadata(&config).unwrap().permissions().mode() & 0o777,
            0o700
        );
    }

    /// No config root leaves the preferences unavailable, never a failed
    /// launch.
    #[test]
    fn no_config_root_is_no_config_folder() {
        let scratch = crate::testutil::Scratch::new();
        let roots = Roots {
            config: None,
            ..crate::testutil::roots(scratch.path())
        };
        assert_eq!(made(&roots).config, None);
    }

    /// The endpoint's directory is the wall around the socket. `/tmp` is shared
    /// and sticky-bit only, so anything that can enter the directory can reach
    /// the address the host is about to dial.
    #[cfg(unix)]
    #[test]
    fn runtime_dir_is_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let dir = runtime_dir(&crate::testutil::roots(scratch.path())).expect("runtime dir");
        let mode = std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o700, "the runtime dir must be owner-only");
    }

    #[cfg(unix)]
    #[test]
    fn the_runtime_root_is_an_absolute_directory() {
        let scratch = crate::testutil::Scratch::new();
        let dir = scratch.path().to_path_buf();

        assert_eq!(runtime_home(Some(dir.clone().into())), Some(dir.clone()));
        // The working directory exists, and is still not taken by a relative name.
        assert_eq!(runtime_home(Some(".".into())), None);
        assert_eq!(runtime_home(Some(dir.join("missing").into())), None);
        assert_eq!(runtime_home(None), None);
    }

    /// Without `$XDG_RUNTIME_DIR` the fallback lands in a temp dir every user
    /// shares, so two users must not name the same directory: the first to run
    /// owns it `0700` and the second cannot chmod it.
    #[cfg(unix)]
    #[test]
    fn runtime_dir_path_is_per_user_outside_xdg() {
        let mine = runtime_dir_path(None, 1000).unwrap();
        let theirs = runtime_dir_path(None, 1001).unwrap();
        assert_ne!(mine, theirs);

        // $XDG_RUNTIME_DIR is already per-user, so the uid stays out of the name.
        let xdg = Path::new("/run/user/1000");
        assert_eq!(
            runtime_dir_path(Some(xdg), 1000).unwrap(),
            xdg.join(RUNTIME_DIR_NAME)
        );
    }

    /// Another user can plant the fallback name as a symlink into a directory of
    /// ours. Refusing it is half the fix; the other half is refusing it *before*
    /// the chmod, or the app tightens the target on the attacker's behalf.
    #[cfg(unix)]
    #[test]
    fn ensure_private_dir_refuses_a_symlink_without_touching_its_target() {
        use std::os::unix::fs::PermissionsExt;

        let base = std::env::temp_dir().join(format!("kubetail-symlink-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let target = base.join("victim");
        std::fs::create_dir_all(&target).expect("create victim");
        std::fs::set_permissions(&target, std::fs::Permissions::from_mode(0o755))
            .expect("loosen victim");

        let planted = base.join("planted");
        std::os::unix::fs::symlink(&target, &planted).expect("plant symlink");

        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };
        assert!(ensure_private_dir(&planted, uid).is_err());
        assert_eq!(
            std::fs::metadata(&target).unwrap().permissions().mode() & 0o777,
            0o755,
            "the symlink's target must be left alone"
        );

        let _ = std::fs::remove_dir_all(&base);
    }

    /// Another user can claim the fallback name before this user's first run.
    /// That must not keep the app from starting: a fresh directory of a random
    /// name, which nobody can claim ahead, takes its place, and what the squatter
    /// planted is left alone.
    #[cfg(unix)]
    #[test]
    fn a_squatted_fallback_gives_way_to_a_fresh_directory() {
        use std::os::unix::fs::PermissionsExt;

        let base = std::env::temp_dir().join(format!("kubetail-squat-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let victim = base.join("victim");
        std::fs::create_dir_all(&victim).expect("create victim");
        std::fs::set_permissions(&victim, std::fs::Permissions::from_mode(0o755))
            .expect("loosen victim");
        let planted = base.join("kubetail-1000");
        std::os::unix::fs::symlink(&victim, &planted).expect("plant symlink");

        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };
        let dir = private_dir_or_fresh(&planted, uid).expect("a fresh directory");

        assert_eq!(dir.parent(), Some(base.as_path()));
        let name = dir.file_name().unwrap().to_string_lossy().into_owned();
        assert!(name.starts_with("kubetail-1000-"), "{name}");
        let meta = std::fs::symlink_metadata(&dir).unwrap();
        assert!(meta.is_dir());
        assert_eq!(meta.permissions().mode() & 0o777, 0o700);
        assert_eq!(
            std::fs::metadata(&victim).unwrap().permissions().mode() & 0o777,
            0o755,
            "the squatter's target must be left alone"
        );

        let _ = std::fs::remove_dir_all(&base);
    }

    /// A later launch under a squatted name keeps to the directory an earlier
    /// one made beside it, so the sidecar's sweep reaches what a crashed run
    /// left there and launches do not pile directories up in the temp dir.
    #[cfg(unix)]
    #[test]
    fn a_later_launch_keeps_the_directory_an_earlier_one_made() {
        let base = std::env::temp_dir().join(format!("kubetail-again-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let victim = base.join("victim");
        std::fs::create_dir_all(&victim).expect("create victim");
        let planted = base.join("kubetail-1000");
        std::os::unix::fs::symlink(&victim, &planted).expect("plant symlink");

        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };
        let first = private_dir_or_fresh(&planted, uid).expect("a fresh directory");
        let second = private_dir_or_fresh(&planted, uid).expect("the same directory");

        assert_eq!(first, second);
        let _ = std::fs::remove_dir_all(&base);
    }

    /// A link named as a fresh directory is named is passed over, never
    /// adopted: whoever planted it owns where it leads.
    #[cfg(unix)]
    #[test]
    fn a_link_named_like_an_earlier_directory_is_passed_over() {
        use std::os::unix::fs::PermissionsExt;

        let base = std::env::temp_dir().join(format!("kubetail-lookalike-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let victim = base.join("victim");
        std::fs::create_dir_all(&victim).expect("create victim");
        std::fs::set_permissions(&victim, std::fs::Permissions::from_mode(0o755))
            .expect("loosen victim");
        let planted = base.join("kubetail-1000");
        std::os::unix::fs::symlink(&victim, &planted).expect("plant symlink");
        let lookalike = base.join("kubetail-1000-aaaaaa");
        std::os::unix::fs::symlink(&victim, &lookalike).expect("plant lookalike");

        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };
        let dir = private_dir_or_fresh(&planted, uid).expect("a fresh directory");

        assert_ne!(dir, lookalike);
        let meta = std::fs::symlink_metadata(&dir).unwrap();
        assert!(meta.is_dir());
        assert_eq!(
            std::fs::metadata(&victim).unwrap().permissions().mode() & 0o777,
            0o755,
            "the lookalike's target must be left alone"
        );

        let _ = std::fs::remove_dir_all(&base);
    }

    /// A fallback name this user owns is kept, since the sidecar sweeps what a
    /// crashed run left in it.
    #[cfg(unix)]
    #[test]
    fn a_fallback_this_user_owns_is_kept() {
        let base = std::env::temp_dir().join(format!("kubetail-owned-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        let dir = base.join("kubetail-1000");
        std::fs::create_dir_all(&dir).expect("create");

        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };
        assert_eq!(private_dir_or_fresh(&dir, uid).expect("adopt"), dir);

        let _ = std::fs::remove_dir_all(&base);
    }

    #[cfg(windows)]
    #[test]
    fn runtime_dir_is_created_in_the_app_folder() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());

        let dir = runtime_dir(&roots).expect("runtime dir");

        assert_eq!(Some(&dir), current_layout(&roots).runtime.as_ref());
        assert!(dir.is_dir());
    }

    #[test]
    fn roots_resolve_on_the_app() {
        let scratch = crate::testutil::Scratch::new();
        let app = crate::testutil::mock_app(
            scratch.path(),
            crate::services::sidecar::SidecarService::without_process(None),
        );
        let roots = Roots::of(app.handle()).unwrap();
        assert!(roots.config.unwrap().is_absolute());
        assert!(roots.local_data.is_absolute());
        assert!(roots.cache.is_absolute());
        assert!(roots.home.is_absolute());
        assert!(roots.state.is_absolute());
    }

    /// A `..` is refused whatever is on disk: a link before it would make the
    /// folder the filesystem opens differ from the one the sidecar fences.
    #[cfg(unix)]
    #[test]
    fn a_root_holding_dotdot_is_refused() {
        let err = plain_root(PathBuf::from("/home/x/link/../conf")).unwrap_err();
        assert!(err.to_string().contains("link/../conf"), "got: {err}");
        assert!(plain_root(PathBuf::from("../conf")).is_err());
    }

    /// Windows takes `..` out by name, as the sidecar does.
    #[cfg(windows)]
    #[test]
    fn a_root_holding_dotdot_stands_on_windows() {
        let root = PathBuf::from(r"C:\Users\x\link\..\conf");
        assert_eq!(plain_root(root.clone()).unwrap(), root);
    }

    /// A root with no `..` stands as given, link or missing: the sidecar
    /// resolves its links itself.
    #[test]
    fn a_root_without_dotdot_stands_as_given() {
        let scratch = crate::testutil::Scratch::new();
        let missing = scratch.path().join("missing");

        assert_eq!(plain_root(missing.clone()).unwrap(), missing);
    }

    /// A refused root fails the launch, the config root included, since the
    /// sidecar fences the config directory by name. Only macOS hangs a
    /// directory off the home.
    #[cfg(unix)]
    #[test]
    fn a_refused_root_fails_the_launch() {
        let scratch = crate::testutil::Scratch::new();
        let fine = crate::testutil::roots(scratch.path());
        let dotted = scratch.path().join("link/../root");
        let with = |config: Option<PathBuf>, local_data: PathBuf, home: PathBuf| {
            Roots::new(
                config,
                local_data,
                fine.cache.clone(),
                home,
                fine.state.clone(),
            )
        };

        assert!(with(
            Some(dotted.clone()),
            fine.local_data.clone(),
            fine.home.clone()
        )
        .is_err());
        assert!(with(fine.config.clone(), dotted.clone(), fine.home.clone()).is_err());
        assert!(Roots::new(
            fine.config.clone(),
            fine.local_data.clone(),
            dotted.clone(),
            fine.home.clone(),
            fine.state.clone()
        )
        .is_err());
        assert_eq!(
            with(fine.config.clone(), fine.local_data.clone(), dotted).is_err(),
            cfg!(target_os = "macos")
        );
        assert!(with(None, fine.local_data.clone(), fine.home.clone())
            .unwrap()
            .config
            .is_none());
    }

    /// `$XDG_RUNTIME_DIR` is a root the host hands the sidecar too.
    #[cfg(unix)]
    #[test]
    fn a_runtime_root_holding_dotdot_is_refused() {
        assert!(runtime_dir_path(Some(Path::new("/run/link/../user")), 1000).is_err());
    }

    /// An owner-only config directory is not chmod-ed again: a setgid bit,
    /// which a chmod to `0700` would clear, survives.
    #[cfg(unix)]
    #[test]
    fn an_owner_only_config_dir_is_not_chmodded() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let dir = scratch.path().join("config");
        std::fs::create_dir(&dir).unwrap();
        std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o2700)).unwrap();
        let mode = || std::fs::metadata(&dir).unwrap().permissions().mode() & 0o7777;
        assert_eq!(mode(), 0o2700, "the setgid bit must be settable here");

        ensure_config_dir(&dir, false).unwrap();

        assert_eq!(mode(), 0o2700);
    }

    /// A config folder that becomes a link after launch keeps its target's
    /// mode at the next write too.
    #[cfg(unix)]
    #[test]
    fn a_config_dir_linked_after_launch_keeps_its_targets_mode() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let target = scratch.path().join("dotfiles");
        std::fs::create_dir(&target).unwrap();
        std::fs::set_permissions(&target, std::fs::Permissions::from_mode(0o755)).unwrap();
        let dir = scratch.path().join("config");
        std::os::unix::fs::symlink(&target, &dir).unwrap();

        ensure_config_dir(&dir, false).unwrap();

        let mode = std::fs::metadata(&target).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o755);
    }

    /// On macOS the state holds the window frame alone, so one that cannot
    /// be made leaves the launch going.
    #[cfg(target_os = "macos")]
    #[test]
    fn a_state_dir_that_cannot_be_made_leaves_a_macos_launch_going() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let state = current_layout(&roots).state;
        std::fs::create_dir_all(state.parent().unwrap()).unwrap();
        std::fs::write(&state, "").unwrap();

        assert!(Folders::make(&roots).is_ok());
    }

    /// A config folder an earlier build left open is tightened, unless it
    /// was a link at launch; one removed since is made again.
    #[cfg(unix)]
    #[test]
    fn an_open_config_dir_is_tightened() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let dir = scratch.path().join("config");
        std::fs::create_dir(&dir).unwrap();
        std::fs::set_permissions(&dir, std::fs::Permissions::from_mode(0o755)).unwrap();
        let mode = || std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777;

        ensure_config_dir(&dir, true).unwrap();
        assert_eq!(mode(), 0o755, "a linked folder's mode is the user's");
        ensure_config_dir(&dir, false).unwrap();
        assert_eq!(mode(), 0o700);

        std::fs::remove_dir(&dir).unwrap();
        ensure_config_dir(&dir, true).unwrap();
        assert_eq!(mode(), 0o700, "a removed folder is made owner-only");
    }

    #[test]
    fn an_unresolvable_root_is_named_in_the_error() {
        let err = resolved(Err(tauri::Error::UnknownPath), "home_dir").unwrap_err();
        assert!(err.to_string().contains("resolve home_dir"), "got: {err}");
    }

    /// XDG ignores a base directory that is not absolute.
    #[test]
    fn the_state_root_is_xdg_state_home_else_under_the_home() {
        let home = Path::new("/home/x");
        let fallback = home.join(".local/state");
        let absolute = std::env::temp_dir();

        assert_eq!(state_home(Some(absolute.clone().into()), home), absolute);
        assert_eq!(state_home(Some("state".into()), home), fallback);
        assert_eq!(state_home(Some("".into()), home), fallback);
        assert_eq!(state_home(None, home), fallback);
    }

    #[cfg(unix)]
    #[test]
    fn state_dir_is_created_owner_only() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let dir = made(&crate::testutil::roots(scratch.path())).state;

        assert_eq!(
            std::fs::metadata(&dir).unwrap().permissions().mode() & 0o777,
            0o700
        );
    }

    /// The sidecar fences the state directory by name, so on Linux a refused
    /// state root fails the launch. Elsewhere it is not read.
    #[cfg(unix)]
    #[test]
    fn a_refused_state_root_fails_the_launch_on_linux() {
        let scratch = crate::testutil::Scratch::new();
        let fine = crate::testutil::roots(scratch.path());

        let roots = Roots::new(
            fine.config,
            fine.local_data,
            fine.cache,
            fine.home,
            scratch.path().join("link/../state"),
        );

        assert_eq!(roots.is_err(), cfg!(target_os = "linux"));
    }

    /// A junction and a directory symlink are each a link to `is_link`, as
    /// they are to the sidecar's `realpath.IsLink`.
    #[cfg(windows)]
    #[test]
    fn a_junction_and_a_directory_symlink_are_links() {
        let scratch = crate::testutil::Scratch::new();
        let target = scratch.path().join("target");
        std::fs::create_dir(&target).unwrap();
        for kind in ["/J", "/D"] {
            let link = scratch.path().join(format!("link{}", &kind[1..]));
            mklink(kind, &target, &link);
            assert!(is_link(&link), "{kind}");
        }
    }

    /// Makes a `kind` link (`/J` a junction, `/D` a directory symlink) at
    /// `link` to `target`, with the folders above it.
    #[cfg(windows)]
    fn mklink(kind: &str, target: &Path, link: &Path) {
        std::fs::create_dir_all(link.parent().unwrap()).unwrap();
        let out = std::process::Command::new("cmd")
            .args(["/c", "mklink", kind])
            .arg(link)
            .arg(target)
            .output()
            .unwrap();
        assert!(out.status.success(), "{out:?}");
    }

    /// A layout folder junctioned out of the profile fails the launch; one
    /// junctioned inside it launches, named by its target, which no
    /// verbatim prefix spells.
    #[cfg(windows)]
    #[test]
    fn a_junction_out_of_the_profile_fails_the_launch() {
        let scratch = crate::testutil::Scratch::new();
        let profile = scratch.path().join("profile");
        let roots = crate::testutil::roots(&profile);
        let app = current_layout(&roots).app.unwrap();

        let outside = scratch.path().join("D").join("Kubetail");
        std::fs::create_dir_all(&outside).unwrap();
        mklink("/J", &outside, &app);
        let err = Folders::make(&roots).unwrap_err().to_string();
        assert!(err.contains("outside your profile"), "got: {err}");

        std::fs::remove_dir(&app).unwrap();
        let inside = profile.join("moved").join("Kubetail");
        std::fs::create_dir_all(&inside).unwrap();
        mklink("/J", &inside, &app);
        let folders = made(&roots);
        assert!(folders.state.starts_with(&inside), "{folders:?}");
        for dir in [
            Some(&folders.state),
            folders.logs.as_ref(),
            folders.config.as_ref(),
        ]
        .into_iter()
        .flatten()
        {
            assert!(
                !dir.to_string_lossy().starts_with(VERBATIM_PREFIX),
                "{dir:?}"
            );
        }
    }

    /// A junction at `<state>/webview` is a link, refused as a symlink is.
    #[cfg(windows)]
    #[test]
    fn a_junctioned_webview_dir_is_refused() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let target = scratch.path().join("elsewhere");
        std::fs::create_dir(&target).unwrap();
        mklink("/J", &target, &current_layout(&roots).state.join("webview"));

        let err = Folders::make(&roots).unwrap_err();

        assert!(err.to_string().contains("is a link"), "got: {err}");
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn a_linked_webview_dir_is_refused() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let target = scratch.path().join("elsewhere");
        std::fs::create_dir(&target).unwrap();
        let state = current_layout(&roots).state;
        std::fs::create_dir_all(&state).unwrap();
        std::os::unix::fs::symlink(&target, state.join("webview")).unwrap();

        let err = Folders::make(&roots).unwrap_err();

        assert!(err.to_string().contains("is a link"), "got: {err}");
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn a_webview_dir_that_is_not_utf8_is_refused() {
        use std::os::unix::ffi::OsStrExt;

        let scratch = crate::testutil::Scratch::new();
        let roots = Roots {
            state: scratch
                .path()
                .join(std::ffi::OsStr::from_bytes(b"st\xffate")),
            ..crate::testutil::roots(scratch.path())
        };

        let err = Folders::make(&roots).unwrap_err();

        assert!(err.to_string().contains("not UTF-8"), "got: {err}");
    }

    /// The sidecar fences the state directory by the path the host keeps its
    /// files under.
    #[test]
    fn the_state_dir_is_named_before_it_is_made() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());

        let named = current_layout(&roots).state;

        assert!(!named.exists());
        assert_eq!(made(&roots).state, named);
    }

    /// The roots Tauri resolves on `platform`, so each platform's layout is
    /// checked on every OS. Windows' are written with `/`, which it reads as
    /// a separator too.
    fn platform_roots(platform: Platform) -> Roots {
        let roots = |config: &str, local_data: &str, cache: &str, home: &str, state: &str| Roots {
            config: Some(PathBuf::from(config)),
            local_data: PathBuf::from(local_data),
            cache: PathBuf::from(cache),
            home: PathBuf::from(home),
            state: PathBuf::from(state),
        };
        match platform {
            Platform::Linux => roots(
                "/home/x/.config",
                "/home/x/.local/share",
                "/home/x/.cache",
                "/home/x",
                "/home/x/.local/state",
            ),
            Platform::MacOs => roots(
                "/Users/x/Library/Application Support",
                "/Users/x/Library/Application Support",
                "/Users/x/Library/Caches",
                "/Users/x",
                "/Users/x/.local/state",
            ),
            Platform::Windows => roots(
                "C:/Users/x/AppData/Roaming",
                "C:/Users/x/AppData/Local",
                "C:/Users/x/AppData/Local",
                "C:/Users/x",
                "C:/Users/x/.local/state",
            ),
        }
    }

    /// One case of `sidecar/internal/app/layout/testdata/layouts.json`, which the sidecar's
    /// `layout` package reads too, so the two layouts cannot drift.
    #[derive(serde::Deserialize)]
    #[serde(deny_unknown_fields)]
    struct LayoutCase {
        name: String,
        platform: Platform,
        dev: bool,
        roots: Roots,
        layout: Layout,
    }

    #[test]
    fn each_platforms_layout_is_the_fixtures() {
        let cases: Vec<LayoutCase> = serde_json::from_str(include_str!(
            "../../sidecar/internal/app/layout/testdata/layouts.json"
        ))
        .unwrap();
        for case in &cases {
            assert_eq!(
                layout_for(case.platform, case.dev, &case.roots),
                case.layout,
                "{}",
                case.name
            );
        }
        for platform in [Platform::Linux, Platform::MacOs, Platform::Windows] {
            assert!(cases.iter().any(|case| case.platform == platform));
        }
    }

    #[test]
    fn linux_gives_each_kind_a_leaf_on_its_own_root() {
        let roots = platform_roots(Platform::Linux);
        let leaf = app_dir_name(Platform::Linux, cfg!(debug_assertions));

        let layout = layout(Platform::Linux, &roots);

        assert_eq!(leaf.trim_end_matches("-dev"), "kubetail");
        assert_eq!(leaf.ends_with("-dev"), cfg!(debug_assertions));
        assert_eq!(layout.app, None);
        assert_eq!(layout.data, roots.local_data.join(leaf));
        assert_eq!(layout.config, Some(roots.config.unwrap().join(leaf)));
        assert_eq!(layout.state, roots.state.join(leaf));
        assert_eq!(layout.cache, roots.cache.join(leaf));
        assert_eq!(layout.logs, roots.state.join(leaf).join("logs"));
        assert_eq!(layout.runtime, None);
    }

    /// macOS's config root is its data root, so the two kinds and the state,
    /// which has no root of its own there, are subfolders of one app folder.
    /// The cache and the logs keep the platform's own folders.
    #[test]
    fn macos_keeps_data_config_and_state_in_one_app_folder() {
        let roots = platform_roots(Platform::MacOs);
        let leaf = app_dir_name(Platform::MacOs, cfg!(debug_assertions));
        let app = roots.local_data.join(leaf);

        let layout = layout(Platform::MacOs, &roots);

        assert_eq!(leaf.trim_end_matches("-dev"), "Kubetail");
        assert_eq!(leaf.ends_with("-dev"), cfg!(debug_assertions));
        assert_eq!(layout.data, app.join("data"));
        assert_eq!(layout.config, Some(app.join("config")));
        assert_eq!(layout.state, app.join("state"));
        assert_eq!(layout.cache, roots.cache.join(leaf));
        assert_eq!(layout.logs, roots.home.join("Library/Logs").join(leaf));
        assert_eq!(layout.runtime, None);
        assert_eq!(layout.app, Some(app));
    }

    /// macOS's config directory is in the app folder even under a config root
    /// that is not the data root, and there is none without a config root.
    #[test]
    fn macos_config_follows_the_app_folder() {
        let mut roots = platform_roots(Platform::MacOs);
        let app = roots
            .local_data
            .join(app_dir_name(Platform::MacOs, cfg!(debug_assertions)));

        roots.config = Some(PathBuf::from("/Users/x/elsewhere"));
        assert_eq!(
            layout(Platform::MacOs, &roots).config,
            Some(app.join("config"))
        );

        roots.config = None;
        assert_eq!(layout(Platform::MacOs, &roots).config, None);
    }

    /// A log folder that is no link and cannot be made leaves `logs`
    /// `None`, and the launch goes on, logging to stderr.
    #[test]
    fn a_log_dir_that_cannot_be_made_is_none() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let logs = current_layout(&roots).logs;
        std::fs::create_dir_all(logs.parent().unwrap()).unwrap();
        std::fs::write(&logs, "").unwrap();

        assert_eq!(made(&roots).logs, None);
    }

    /// On Linux and Windows a state folder that cannot be made fails the
    /// launch: it holds the logs and the webview's profile.
    #[cfg(not(target_os = "macos"))]
    #[test]
    fn a_state_dir_that_cannot_be_made_fails_the_launch() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let state = current_layout(&roots).state;
        std::fs::create_dir_all(state.parent().unwrap()).unwrap();
        std::fs::write(&state, "").unwrap();

        assert!(Folders::make(&roots).is_err());
    }

    #[test]
    fn the_running_os_takes_its_own_layout() {
        #[cfg(target_os = "linux")]
        assert_eq!(Platform::CURRENT, Platform::Linux);
        #[cfg(target_os = "macos")]
        assert_eq!(Platform::CURRENT, Platform::MacOs);
        #[cfg(windows)]
        assert_eq!(Platform::CURRENT, Platform::Windows);
    }

    /// The logs sit in the state directory on Linux, so asking for them
    /// tightens one an earlier run left open.
    #[cfg(target_os = "linux")]
    #[test]
    fn log_dir_tightens_the_state_directory() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let state = current_layout(&roots).state;
        std::fs::create_dir_all(&state).unwrap();
        std::fs::set_permissions(&state, std::fs::Permissions::from_mode(0o755)).unwrap();

        made(&roots);

        let mode = std::fs::metadata(&state).unwrap().permissions().mode();
        assert_eq!(mode & 0o777, 0o700);
    }

    /// Windows' cache root is its local data root, and it has no state or
    /// runtime root, so those kinds are siblings in one app folder, the logs
    /// in the state as on Linux. The config roams, in a root of its own.
    #[test]
    fn windows_keeps_every_local_kind_side_by_side_in_one_app_folder() {
        let roots = platform_roots(Platform::Windows);
        let leaf = app_dir_name(Platform::Windows, cfg!(debug_assertions));
        let app = roots.local_data.join(leaf);

        let layout = layout(Platform::Windows, &roots);

        assert_eq!(leaf.trim_end_matches("-dev"), "Kubetail");
        assert_eq!(leaf.ends_with("-dev"), cfg!(debug_assertions));
        assert_eq!(layout.data, app.join("data"));
        assert_eq!(layout.cache, app.join("cache"));
        assert_eq!(layout.state, app.join("state"));
        assert_eq!(layout.logs, app.join("state").join("logs"));
        assert_eq!(layout.runtime, Some(app.join("run")));
        let app_on_config_root = roots.config.unwrap().join(leaf);
        assert_eq!(layout.config, Some(app_on_config_root.join("config")));
        assert_eq!(layout.app_on_config_root, Some(app_on_config_root));
        assert_eq!(layout.app, Some(app));
    }

    /// Windows' roaming and local roots resolved to one folder.
    fn windows_shared_roots() -> Roots {
        let mut roots = platform_roots(Platform::Windows);
        roots.config = Some(roots.local_data.clone());
        roots
    }

    /// The config is a subfolder of its leaf, so on the local data root it is
    /// a sibling of the other kinds, not the app folder that holds them.
    #[test]
    fn windows_config_on_the_local_root_is_a_subfolder_of_the_app_folder() {
        let roots = windows_shared_roots();
        let app = roots
            .local_data
            .join(app_dir_name(Platform::Windows, cfg!(debug_assertions)));

        let layout = layout(Platform::Windows, &roots);

        assert_eq!(layout.config, Some(app.join("config")));
    }

    /// Each kind is fenced and deleted on its own, so none may be another's
    /// folder or sit inside one.
    #[test]
    fn no_kind_shares_or_holds_another_kinds_folder() {
        let cases = [
            (Platform::Linux, platform_roots(Platform::Linux)),
            (Platform::MacOs, platform_roots(Platform::MacOs)),
            (Platform::Windows, platform_roots(Platform::Windows)),
            (Platform::Windows, windows_shared_roots()),
        ];
        for (platform, roots) in cases {
            let layout = layout(platform, &roots);
            let mut kinds = vec![
                ("data", layout.data),
                ("state", layout.state),
                ("cache", layout.cache),
            ];
            kinds.extend(layout.config.map(|dir| ("config", dir)));
            kinds.extend(layout.runtime.map(|dir| ("runtime", dir)));
            for (name, dir) in &kinds {
                for (other, other_dir) in &kinds {
                    assert!(
                        name == other || !other_dir.starts_with(dir),
                        "{platform:?}: {other} {other_dir:?} is in {name} {dir:?}"
                    );
                }
            }
        }
    }

    /// An open app folder is tightened before a kind in it is made, and one
    /// already owner-only keeps its other bits.
    #[cfg(unix)]
    #[test]
    fn the_app_folder_is_made_owner_only_before_a_kind_in_it() {
        use std::os::unix::fs::PermissionsExt;

        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let layout = layout(Platform::MacOs, &roots);
        let app = layout.app.clone().unwrap();
        let mode = || std::fs::metadata(&app).unwrap().permissions().mode() & 0o7777;
        std::fs::create_dir_all(&app).unwrap();
        std::fs::set_permissions(&app, std::fs::Permissions::from_mode(0o775)).unwrap();

        ensure_app_dir_above(&layout, &layout.config.clone().unwrap()).unwrap();
        assert_eq!(mode(), 0o700);

        std::fs::set_permissions(&app, std::fs::Permissions::from_mode(0o2700)).unwrap();
        ensure_app_dir_above(&layout, &layout.data).unwrap();
        assert_eq!(mode(), 0o2700);
    }

    /// A kind outside the app folder leaves it alone.
    #[test]
    fn a_kind_outside_the_app_folder_does_not_make_it() {
        let scratch = crate::testutil::Scratch::new();
        let roots = crate::testutil::roots(scratch.path());
        let layout = layout(Platform::MacOs, &roots);

        ensure_app_dir_above(&layout, &layout.cache).unwrap();

        assert!(!layout.app.unwrap().exists());
    }

    #[cfg(unix)]
    #[test]
    fn a_private_dir_is_made_where_none_was() {
        let scratch = crate::testutil::Scratch::new();
        let dir = scratch.path().join("run");
        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };

        ensure_private_dir(&dir, uid).unwrap();

        assert!(dir.is_dir());
    }

    /// A parent that is not there fails both ways of making the directory.
    #[cfg(unix)]
    #[test]
    fn a_private_dir_under_a_missing_parent_is_an_error() {
        let scratch = crate::testutil::Scratch::new();
        let dir = scratch.path().join("missing").join("run");
        // SAFETY: `geteuid` reads process state and cannot fail.
        let uid = unsafe { libc::geteuid() };

        assert!(ensure_private_dir(&dir, uid).is_err());
        assert!(fresh_private_dir(&dir).is_err());
    }
}
