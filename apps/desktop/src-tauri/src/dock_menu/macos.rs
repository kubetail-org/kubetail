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

//! Patches the Dock menu and the Quit reply onto AppKit, once during app
//! setup. tao's `NSApplication` delegate implements neither
//! `applicationDockMenu:` nor `applicationShouldTerminate:`, so `install` adds
//! both to its live class. Wiring alone: AppKit runs it on the main thread,
//! which no test runs on, so coverage ignores this file. What an item does and
//! how a quit is answered are decided in `dock_menu/mod.rs`.

use std::ffi::CStr;
use std::sync::OnceLock;

use objc2::rc::Retained;
use objc2::runtime::{AnyClass, AnyObject, Imp, Sel};
use objc2::{define_class, msg_send, sel, ClassType};
use objc2_app_kit::{NSApplication, NSMenu, NSMenuItem};
use objc2_foundation::{MainThreadMarker, NSAppleEventManager, NSString};
use tauri::AppHandle;

use super::{is_system_quit_event, run_action, terminate_reply, DockAction, K_AE_QUIT_REASON};

/// Wraps a non-`Sync` value so it can live in a `static`. Sound because every
/// access is on the main thread — `install` writes it, AppKit's main-thread
/// callbacks read it.
struct MainThreadStatic<T>(T);

// SAFETY: see the type-level comment — access is main-thread-only by construction.
unsafe impl<T> Send for MainThreadStatic<T> {}
unsafe impl<T> Sync for MainThreadStatic<T> {}

/// Process-wide handle so the Objective-C action callbacks can reach Tauri.
static APP_HANDLE: OnceLock<AppHandle> = OnceLock::new();

/// Keeps the Dock `NSMenu` alive for the process lifetime; `applicationDockMenu:`
/// returns a borrowed pointer into it that AppKit does not retain.
static DOCK_MENU: OnceLock<MainThreadStatic<Retained<NSMenu>>> = OnceLock::new();

/// Keeps the action target alive — `NSMenuItem.target` is weak; without this,
/// items fire into freed memory.
static MENU_TARGET: OnceLock<MainThreadStatic<Retained<DockMenuTarget>>> = OnceLock::new();

define_class!(
    /// Action target for the Dock menu items. Stateless — callbacks reach Tauri
    /// through [`APP_HANDLE`].
    #[unsafe(super(objc2::runtime::NSObject))]
    #[name = "KubetailDockMenuTarget"]
    struct DockMenuTarget;

    impl DockMenuTarget {
        #[unsafe(method(newWindow:))]
        fn new_window(&self, _sender: Option<&AnyObject>) {
            run_action(APP_HANDLE.get(), DockAction::NewWindow);
        }

        #[unsafe(method(showMainWindow:))]
        fn show_main_window(&self, _sender: Option<&AnyObject>) {
            run_action(APP_HANDLE.get(), DockAction::ShowMainWindow);
        }
    }
);

/// Install the Dock menu. Main-thread only; idempotent; patch failures are
/// logged and swallowed (not worth aborting startup).
pub fn install(app: &AppHandle) {
    let Some(mtm) = MainThreadMarker::new() else {
        tracing::error!("dock menu install() called off the main thread");
        return;
    };

    // Storing the handle is the gate for everything else; bail if already done.
    if APP_HANDLE.set(app.clone()).is_err() {
        return;
    }

    let target: Retained<DockMenuTarget> = unsafe { msg_send![DockMenuTarget::class(), new] };
    let menu = build_menu(mtm, &target);

    let _ = MENU_TARGET.set(MainThreadStatic(target));
    let _ = DOCK_MENU.set(MainThreadStatic(menu));

    if let Err(err) = patch_delegate_class() {
        tracing::error!(
            err,
            "failed to install applicationDockMenu: on tao delegate"
        );
    }
}

/// Build the Dock `NSMenu`: New Window, Show Main Window. No Quit — AppKit
/// appends its own (whose `terminate:` is caught by
/// [`application_should_terminate`]).
fn build_menu(mtm: MainThreadMarker, target: &DockMenuTarget) -> Retained<NSMenu> {
    let menu = NSMenu::new(mtm);

    for (title, action) in [
        ("New Window", sel!(newWindow:)),
        ("Show Main Window", sel!(showMainWindow:)),
    ] {
        let item = NSMenuItem::new(mtm);
        item.setTitle(&NSString::from_str(title));
        unsafe {
            item.setAction(Some(action));
            item.setTarget(Some(target));
        }
        menu.addItem(&item);
    }

    menu
}

/// Add `applicationDockMenu:` and `applicationShouldTerminate:` to tao's live
/// delegate class (tao 0.35 implements neither).
fn patch_delegate_class() -> Result<(), &'static str> {
    let mtm = MainThreadMarker::new().ok_or("not on main thread")?;
    let app = NSApplication::sharedApplication(mtm);

    let delegate = app.delegate().ok_or("NSApplication has no delegate")?;

    // Via AnyObject so it works regardless of the protocol-object wrapper type.
    let delegate_obj: &AnyObject = delegate.as_ref();
    let class: &AnyClass = delegate_obj.class();

    // `Imp` is `extern "C-unwind" fn()`; transmute each typed IMP to it. Type
    // encodings: receiver, selector, one object arg, return (`@` menu object,
    // `Q` NSUInteger terminate reply).
    let dock_menu_imp: Imp = unsafe {
        std::mem::transmute::<
            unsafe extern "C-unwind" fn(*mut AnyObject, Sel, *mut AnyObject) -> *mut AnyObject,
            Imp,
        >(application_dock_menu)
    };
    add_method_if_absent(class, sel!(applicationDockMenu:), dock_menu_imp, c"@@:@")?;

    let should_terminate_imp: Imp = unsafe {
        std::mem::transmute::<
            unsafe extern "C-unwind" fn(*mut AnyObject, Sel, *mut AnyObject) -> usize,
            Imp,
        >(application_should_terminate)
    };
    add_method_if_absent(
        class,
        sel!(applicationShouldTerminate:),
        should_terminate_imp,
        c"Q@:@",
    )?;

    Ok(())
}

/// Add `imp` for `sel` unless the class already implements it — guards against
/// a double patch and leaves any tao-provided implementation untouched.
fn add_method_if_absent(
    class: &AnyClass,
    sel: Sel,
    imp: Imp,
    types: &CStr,
) -> Result<(), &'static str> {
    if class.instance_method(sel).is_some() {
        return Ok(());
    }

    let added = unsafe {
        objc2::ffi::class_addMethod(
            (class as *const AnyClass).cast_mut(),
            sel,
            imp,
            types.as_ptr(),
        )
    };

    if added.as_bool() {
        Ok(())
    } else {
        Err("class_addMethod rejected a delegate method")
    }
}

/// `applicationDockMenu:` IMP. Returns a borrowed (non-retained) pointer to the
/// shared Dock menu; AppKit does not take ownership.
unsafe extern "C-unwind" fn application_dock_menu(
    _self: *mut AnyObject,
    _cmd: Sel,
    _sender: *mut AnyObject,
) -> *mut AnyObject {
    match DOCK_MENU.get() {
        Some(menu) => Retained::as_ptr(&menu.0).cast_mut().cast(),
        None => std::ptr::null_mut(),
    }
}

/// `applicationShouldTerminate:` IMP; the reply is [`terminate_reply`]'s.
unsafe extern "C-unwind" fn application_should_terminate(
    _self: *mut AnyObject,
    _cmd: Sel,
    _sender: *mut AnyObject,
) -> usize {
    let exit = APP_HANDLE.get().map(|app| || app.exit(0));
    terminate_reply(is_system_initiated_quit(), exit)
}

/// Whether the in-flight termination came from the OS (logout/restart/
/// shutdown), read off the current Apple Event.
fn is_system_initiated_quit() -> bool {
    let manager = NSAppleEventManager::sharedAppleEventManager();
    let Some(event) = manager.currentAppleEvent() else {
        return false;
    };
    is_system_quit_event(
        event.eventID(),
        event.paramDescriptorForKeyword(K_AE_QUIT_REASON).is_some(),
    )
}
