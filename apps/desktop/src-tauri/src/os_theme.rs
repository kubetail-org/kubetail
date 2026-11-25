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

//! Reads the OS color scheme to resolve the `system` preference *before* the
//! first window exists — Tauri only exposes a theme on an already-built window.
//! Read on the opaque platforms alone (Linux windows are transparent); the
//! readings' meanings compile everywhere so every platform's tests cover them.
//! Every failure path must degrade to light: a wrong answer costs one frame of
//! the wrong — but never white — background.

/// Whether the OS is currently in dark mode.
///
/// Read via `NSUserDefaults` (not `NSAppearance`) so it works before `NSApp`
/// exists.
#[cfg(target_os = "macos")]
pub fn prefers_dark() -> bool {
    use objc2_foundation::{NSString, NSUserDefaults};

    let defaults = NSUserDefaults::standardUserDefaults();
    let key = NSString::from_str("AppleInterfaceStyle");
    let style = defaults.stringForKey(&key).map(|style| style.to_string());
    interface_style_is_dark(style.as_deref())
}

/// macOS `AppleInterfaceStyle`: absent in light mode, `"Dark"` in dark.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn interface_style_is_dark(style: Option<&str>) -> bool {
    style.is_some_and(|style| style.eq_ignore_ascii_case("dark"))
}

/// Windows `AppsUseLightTheme`, `None` when it could not be read: `0` is
/// dark, `1` light. A missing value (older Windows, or a profile that never
/// set it) reads as light.
#[cfg_attr(not(windows), allow(dead_code))]
fn apps_use_light_theme_is_dark(value: Option<u32>) -> bool {
    value == Some(0)
}

/// Whether the OS is currently in dark mode, from the per-user app-theme flag.
#[cfg(target_os = "windows")]
pub fn prefers_dark() -> bool {
    use std::ffi::c_void;

    use windows_sys::Win32::Foundation::ERROR_SUCCESS;
    use windows_sys::Win32::System::Registry::{RegGetValueW, HKEY_CURRENT_USER, RRF_RT_REG_DWORD};

    /// NUL-terminated UTF-16, as the `W` registry APIs expect.
    fn wide(s: &str) -> Vec<u16> {
        s.encode_utf16().chain(std::iter::once(0)).collect()
    }

    let subkey = wide(r"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize");
    let value = wide("AppsUseLightTheme");
    let mut data: u32 = 0;
    let mut size = std::mem::size_of::<u32>() as u32;

    // SAFETY: `subkey`/`value` are NUL-terminated and outlive the call;
    // `RRF_RT_REG_DWORD` constrains the write to `pvdata` to a `DWORD`, which
    // `size` matches; null `pdwtype` skips the type report.
    let status = unsafe {
        RegGetValueW(
            HKEY_CURRENT_USER,
            subkey.as_ptr(),
            value.as_ptr(),
            RRF_RT_REG_DWORD,
            std::ptr::null_mut(),
            std::ptr::from_mut(&mut data).cast::<c_void>(),
            &mut size,
        )
    };

    apps_use_light_theme_is_dark((status == ERROR_SUCCESS).then_some(data))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_dark_interface_style_is_dark() {
        assert!(interface_style_is_dark(Some("Dark")));
        assert!(interface_style_is_dark(Some("dark")));
        assert!(!interface_style_is_dark(Some("Light")));
        assert!(!interface_style_is_dark(None));
    }

    #[test]
    fn only_a_read_zero_light_theme_flag_is_dark() {
        assert!(apps_use_light_theme_is_dark(Some(0)));
        assert!(!apps_use_light_theme_is_dark(Some(1)));
        assert!(!apps_use_light_theme_is_dark(None));
    }

    /// The real read answers either way on a CI runner; what it must not do is
    /// fail.
    #[cfg(any(target_os = "macos", windows))]
    #[test]
    fn the_os_setting_reads_without_failing() {
        let _ = prefers_dark();
    }
}
