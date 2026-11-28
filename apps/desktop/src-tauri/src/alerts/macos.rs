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

//! The macOS notification center: the permission's prompt and reading, each
//! notification, and its press. Wiring alone: the center serves only a signed
//! app in its bundle, which no test runs as, so coverage ignores this file.
//! What an answer and a press mean is decided in `alerts/`.
//!
//! The center raises an exception when a bare binary asks it anything, as
//! `tauri dev` runs the host, so outside a bundle nothing here calls it and
//! the answer is `NotDetermined`.

use std::ptr::NonNull;
use std::sync::OnceLock;

use block2::RcBlock;
use objc2::rc::Retained;
use objc2::runtime::{Bool, NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, ClassType};
use objc2_foundation::{NSBundle, NSError, NSString};
use objc2_user_notifications::{
    UNAuthorizationOptions, UNMutableNotificationContent, UNNotification,
    UNNotificationPresentationOptions, UNNotificationRequest, UNNotificationResponse,
    UNNotificationSettings, UNUserNotificationCenter, UNUserNotificationCenterDelegate,
};
use tauri::{AppHandle, Manager};

use super::{pressed, Notice, NotificationPermission, TITLE};
use crate::state::AppState;

/// Process-wide handle so the center's callbacks can reach Tauri. Set only in
/// a bundle, so it also says the center may be called.
static APP_HANDLE: OnceLock<AppHandle> = OnceLock::new();

define_class!(
    /// The center's delegate: shows a notification while the app is in front,
    /// and hands a press to [`pressed`].
    #[unsafe(super(NSObject))]
    #[name = "KubetailNotificationDelegate"]
    struct NotificationDelegate;

    unsafe impl NSObjectProtocol for NotificationDelegate {}

    unsafe impl UNUserNotificationCenterDelegate for NotificationDelegate {
        #[unsafe(method(userNotificationCenter:willPresentNotification:withCompletionHandler:))]
        fn will_present(
            &self,
            _center: &UNUserNotificationCenter,
            _notification: &UNNotification,
            completion_handler: &block2::DynBlock<dyn Fn(UNNotificationPresentationOptions)>,
        ) {
            completion_handler
                .call((UNNotificationPresentationOptions::Banner
                    | UNNotificationPresentationOptions::List,));
        }

        #[unsafe(method(userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:))]
        fn did_receive(
            &self,
            _center: &UNUserNotificationCenter,
            response: &UNNotificationResponse,
            completion_handler: &block2::DynBlock<dyn Fn()>,
        ) {
            let id = response.notification().request().identifier().to_string();
            if let Some(app) = APP_HANDLE.get() {
                drop(pressed(app, &id));
            }
            completion_handler.call(());
        }
    }
);

/// Whether the host runs as an app in its bundle, the one the center serves.
fn in_bundle() -> bool {
    NSBundle::mainBundle().bundleIdentifier().is_some()
}

/// Sets the delegate, then reads the answer, asking for it the first time.
/// Main thread, during `setup`; outside a bundle the answer is `NotDetermined`.
pub fn install(app: &AppHandle) {
    if !in_bundle() {
        app.state::<AppState>()
            .notification_permission
            .set(NotificationPermission::NotDetermined);
        return;
    }
    if APP_HANDLE.set(app.clone()).is_err() {
        return;
    }
    let delegate: Retained<NotificationDelegate> =
        unsafe { msg_send![NotificationDelegate::class(), new] };
    UNUserNotificationCenter::currentNotificationCenter()
        .setDelegate(Some(ProtocolObject::from_ref(&*delegate)));
    // The center holds its delegate weakly; this one lives as long as the process.
    std::mem::forget(delegate);
    read(true);
}

/// Reads the answer again, as a window gains focus: the user may have changed
/// it in System Settings.
pub fn reread() {
    if APP_HANDLE.get().is_some() {
        read(false);
    }
}

/// Reads the answer into the watch; one not determined yet is asked for when
/// `ask`, which reads it again once answered.
fn read(ask: bool) {
    let block = RcBlock::new(move |settings: NonNull<UNNotificationSettings>| {
        // SAFETY: the center hands the handler a live settings object.
        let status = unsafe { settings.as_ref() }.authorizationStatus();
        let answer = NotificationPermission::from_authorization_status(status.0);
        if let Some(app) = APP_HANDLE.get() {
            app.state::<AppState>().notification_permission.set(answer);
        }
        if ask && answer == NotificationPermission::NotDetermined {
            request();
        }
    });
    UNUserNotificationCenter::currentNotificationCenter()
        .getNotificationSettingsWithCompletionHandler(&block);
}

/// Shows the first launch's prompt, then reads its answer.
fn request() {
    let block = RcBlock::new(|_granted: Bool, _error: *mut NSError| read(false));
    UNUserNotificationCenter::currentNotificationCenter()
        .requestAuthorizationWithOptions_completionHandler(
            UNAuthorizationOptions::Alert | UNAuthorizationOptions::Sound,
            &block,
        );
}

/// Adds `notice` to the center, keyed by its id, which its press hands back.
pub fn show(notice: &Notice) {
    if APP_HANDLE.get().is_none() {
        return;
    }
    let content = UNMutableNotificationContent::new();
    content.setTitle(&NSString::from_str(TITLE));
    content.setBody(&NSString::from_str(&notice.body));
    let request = UNNotificationRequest::requestWithIdentifier_content_trigger(
        &NSString::from_str(&notice.id),
        &content,
        None,
    );
    let done = RcBlock::new(|error: *mut NSError| {
        if !error.is_null() {
            tracing::warn!("macOS refused a notification");
        }
    });
    UNUserNotificationCenter::currentNotificationCenter()
        .addNotificationRequest_withCompletionHandler(&request, Some(&done));
}
