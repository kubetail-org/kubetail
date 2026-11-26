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

// Synchronous platform detection — callers decide layout at first render, so no
// async `@tauri-apps/plugin-os` round-trip. The system WebView's UA is fixed
// per-OS, so sniffing is reliable here.

/** True when running inside the macOS system WebView. */
export function isMacOS(): boolean {
  return /Mac/i.test(window.navigator.userAgent);
}

/**
 * True inside the Linux system WebView — the only frameless *and* transparent
 * window (see `WindowFrame`). A positive sniff keeps unrecognized platforms on
 * the safe opaque, full-bleed path.
 */
export function isLinux(): boolean {
  return /Linux/i.test(window.navigator.userAgent);
}

/** True inside the Windows system WebView, where Kubetail has no sandbox. */
export function isWindows(): boolean {
  return /Windows/i.test(window.navigator.userAgent);
}
