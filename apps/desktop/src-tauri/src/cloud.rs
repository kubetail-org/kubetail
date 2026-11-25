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

//! The kubetail cloud pages the app links out to. One home for the base URL, so the
//! tray's account item and the webview's account menu can't drift apart.

/// Base URL for the kubetail cloud dashboard.
const DASHBOARD_URL: &str = "https://app.kubetail.com";

/// The signed-in user's account page.
pub(crate) fn account_url() -> String {
    format!("{DASHBOARD_URL}/account")
}

#[cfg(test)]
mod tests {
    use super::account_url;

    #[test]
    fn account_url_is_the_dashboard_account_page() {
        assert_eq!(account_url(), "https://app.kubetail.com/account");
    }
}
