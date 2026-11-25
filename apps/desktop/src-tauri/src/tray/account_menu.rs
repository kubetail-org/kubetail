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

//! Pure, unit-tested account-section logic: menu-id constants, the
//! account-state descriptor, and the converter from a gRPC `AuthState`
//! snapshot. Tray wiring lives in [`super`].

use crate::services::sidecar::AuthState;

/// Tray menu item id for the "Login / Create Account" item shown while signed out.
pub const ACCOUNT_LOGIN_ID: &str = "account_login";

/// Tray menu item id for the "Account Settings" item inside the signed-in submenu.
pub const ACCOUNT_SETTINGS_ID: &str = "account_settings";

/// Tray menu item id for the "Sign out" item inside the signed-in submenu.
pub const ACCOUNT_LOGOUT_ID: &str = "account_logout";

/// A minimal projection of the gRPC `AuthState` the account tray menu needs —
/// no tokens, only the identity claims the host renders.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct AccountSnapshot {
    pub authenticated: bool,
    pub name: Option<String>,
    pub email: Option<String>,
}

impl AccountSnapshot {
    pub fn signed_out() -> Self {
        Self::default()
    }

    #[cfg(test)]
    pub fn signed_in(name: Option<&str>, email: Option<&str>) -> Self {
        Self {
            authenticated: true,
            name: name.filter(|s| !s.is_empty()).map(str::to_string),
            email: email.filter(|s| !s.is_empty()).map(str::to_string),
        }
    }
}

/// The account section of the tray menu: either a single "Login / Create
/// Account" item (signed-out) or a submenu titled by the user (signed-in).
#[derive(Debug, PartialEq, Eq)]
pub enum AccountMenuDescriptor {
    /// The user is signed out. Render a single "Login / Create Account" item.
    SignedOut,
    /// The user is signed in. Render a submenu titled `title` containing
    /// "Account Settings" and "Sign out".
    SignedIn { title: String },
}

/// Chooses the best display title for the signed-in submenu: name first, then
/// email, then the generic fallback `"Account"`. Mirrors the precedence used by
/// the webview's `profile-menu.tsx` (`identity?.name || identity?.email`).
pub fn title_for(name: Option<&str>, email: Option<&str>) -> String {
    name.filter(|s| !s.is_empty())
        .or_else(|| email.filter(|s| !s.is_empty()))
        .unwrap_or("Account")
        .to_string()
}

/// Builds the [`AccountMenuDescriptor`] from an [`AccountSnapshot`].
pub fn build_account_descriptor(snap: &AccountSnapshot) -> AccountMenuDescriptor {
    if snap.authenticated {
        AccountMenuDescriptor::SignedIn {
            title: title_for(snap.name.as_deref(), snap.email.as_deref()),
        }
    } else {
        AccountMenuDescriptor::SignedOut
    }
}

/// Converts a gRPC `AuthState` proto snapshot into an [`AccountSnapshot`].
/// Empty-string proto fields are mapped to `None` so the tray logic never
/// renders an empty string as a label.
pub fn account_snapshot_from(state: &AuthState) -> AccountSnapshot {
    let (name, email) = state
        .identity
        .as_ref()
        .map(|id| {
            (
                Some(id.name.clone()).filter(|s| !s.is_empty()),
                Some(id.email.clone()).filter(|s| !s.is_empty()),
            )
        })
        .unwrap_or((None, None));
    AccountSnapshot {
        authenticated: state.authenticated,
        name,
        email,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::services::sidecar::{AuthState, Identity};

    // --- id constants ---

    #[test]
    fn ids_are_distinct_and_stable() {
        // Ids must not collide with each other or the tray_* namespace.
        assert_eq!(ACCOUNT_LOGIN_ID, "account_login");
        assert_eq!(ACCOUNT_SETTINGS_ID, "account_settings");
        assert_eq!(ACCOUNT_LOGOUT_ID, "account_logout");
        // Distinct from each other.
        assert_ne!(ACCOUNT_LOGIN_ID, ACCOUNT_SETTINGS_ID);
        assert_ne!(ACCOUNT_LOGIN_ID, ACCOUNT_LOGOUT_ID);
        assert_ne!(ACCOUNT_SETTINGS_ID, ACCOUNT_LOGOUT_ID);
        // Don't match the tray_ prefix.
        assert!(!ACCOUNT_LOGIN_ID.starts_with("tray_"));
    }

    // --- title_for ---

    #[test]
    fn title_for_prefers_name() {
        assert_eq!(
            title_for(Some("Ada Lovelace"), Some("a@x.com")),
            "Ada Lovelace"
        );
    }

    #[test]
    fn title_for_falls_back_to_email() {
        assert_eq!(title_for(None, Some("a@x.com")), "a@x.com");
        assert_eq!(title_for(Some(""), Some("a@x.com")), "a@x.com");
    }

    #[test]
    fn title_for_falls_back_to_account() {
        assert_eq!(title_for(None, None), "Account");
        assert_eq!(title_for(Some(""), Some("")), "Account");
    }

    // --- build_account_descriptor ---

    #[test]
    fn signed_out_snapshot_produces_signed_out_descriptor() {
        let d = build_account_descriptor(&AccountSnapshot::signed_out());
        assert_eq!(d, AccountMenuDescriptor::SignedOut);
    }

    #[test]
    fn signed_in_snapshot_produces_signed_in_with_name() {
        let snap = AccountSnapshot::signed_in(Some("Ada Lovelace"), Some("a@x.com"));
        assert_eq!(
            build_account_descriptor(&snap),
            AccountMenuDescriptor::SignedIn {
                title: "Ada Lovelace".into()
            }
        );
    }

    #[test]
    fn signed_in_without_name_uses_email() {
        let snap = AccountSnapshot::signed_in(None, Some("a@x.com"));
        assert_eq!(
            build_account_descriptor(&snap),
            AccountMenuDescriptor::SignedIn {
                title: "a@x.com".into()
            }
        );
    }

    #[test]
    fn signed_in_without_any_identity_uses_fallback() {
        let snap = AccountSnapshot {
            authenticated: true,
            name: None,
            email: None,
        };
        assert_eq!(
            build_account_descriptor(&snap),
            AccountMenuDescriptor::SignedIn {
                title: "Account".into()
            }
        );
    }

    // --- account_snapshot_from ---

    #[test]
    fn snapshot_from_auth_state_unauthenticated() {
        let state = AuthState {
            authenticated: false,
            identity: None,
            restoring: false,
        };
        let snap = account_snapshot_from(&state);
        assert!(!snap.authenticated);
        assert!(snap.name.is_none());
        assert!(snap.email.is_none());
    }

    #[test]
    fn snapshot_from_auth_state_authenticated_with_identity() {
        let state = AuthState {
            authenticated: true,
            identity: Some(Identity {
                user_id: "u1".to_string(),
                email: "ada@example.com".to_string(),
                name: "Ada".to_string(),
            }),
            restoring: false,
        };
        let snap = account_snapshot_from(&state);
        assert!(snap.authenticated);
        assert_eq!(snap.name, Some("Ada".to_string()));
        assert_eq!(snap.email, Some("ada@example.com".to_string()));
    }

    #[test]
    fn snapshot_from_auth_state_empty_strings_become_none() {
        let state = AuthState {
            authenticated: true,
            identity: Some(Identity {
                user_id: "u1".to_string(),
                email: String::new(),
                name: String::new(),
            }),
            restoring: false,
        };
        let snap = account_snapshot_from(&state);
        assert!(snap.name.is_none());
        assert!(snap.email.is_none());
    }
}
