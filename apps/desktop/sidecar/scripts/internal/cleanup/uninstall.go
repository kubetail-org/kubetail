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

package cleanup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
)

// Release is the installed release build.
var Release = Build{}

// KeyringService is the release's keyring service, src-tauri's
// KEYCHAIN_SERVICE.
const KeyringService = "Kubetail"

// bundleID is tauri.conf.json's identifier, under which WKWebView keeps the
// webview profile on macOS.
const bundleID = "com.kubetail.desktop"

// productName is tauri.conf.json's productName: the macOS bundle's name and
// the Windows installers' DisplayName.
const productName = "Kubetail"

// linuxPackage is tauri.linux.conf.json's productName, the .deb's and the
// .rpm's package name.
const linuxPackage = "kubetail-desktop"

// UninstallPaths is every folder and file a release keeps on platform, from
// its layout l: each top folder, on macOS WKWebView's folders under the bundle
// id, and the Unix runtime directories. home is the user's home, read on
// macOS alone.
func UninstallPaths(platform layout.Platform, l layout.Layout, home string, runtimeDirs []string) []string {
	paths := TopDirs(l)
	if platform == layout.MacOS {
		paths = append(paths, webKitPaths(home)...)
	}
	return append(paths, runtimeDirs...)
}

// webKitPaths is what WKWebView keeps for the app under home. A dev build
// shares them, since it runs under the same bundle id.
func webKitPaths(home string) []string {
	lib := filepath.Join(home, "Library")
	return []string{
		filepath.Join(lib, "WebKit", bundleID),
		filepath.Join(lib, "Caches", bundleID),
		filepath.Join(lib, "HTTPStorages", bundleID),
		filepath.Join(lib, "HTTPStorages", bundleID+".binarycookies"),
		filepath.Join(lib, "Saved Application State", bundleID+".savedState"),
	}
}

// Existing is each of paths that exists, a link included.
func Existing(paths []string) ([]string, error) {
	var found []string
	for _, path := range paths {
		_, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		found = append(found, path)
	}
	return found, nil
}

// OwnedDirs is each of dirs that is a directory, not a link, owned by this
// user. A runtime directory's name under the shared temp dir is predictable,
// so another user may hold one.
func OwnedDirs(dirs []string) []string {
	var owned []string
	for _, dir := range dirs {
		info, err := os.Lstat(dir)
		if err == nil && info.IsDir() && ownedByMe(info) {
			owned = append(owned, dir)
		}
	}
	return owned
}

// macOSApps is where an installed bundle may be.
func macOSApps(home string) []string {
	return []string{
		filepath.Join("/Applications", productName+".app"),
		filepath.Join(home, "Applications", productName+".app"),
	}
}

// linuxRemoveCommand removes the package through manager, apt-get or dnf, so
// its prerm.sh unloads the AppArmor profiles.
func linuxRemoveCommand(manager string, root bool) []string {
	cmd := []string{manager, "remove", "-y", linuxPackage}
	if !root {
		cmd = append([]string{"sudo"}, cmd...)
	}
	return cmd
}

// appImages is each AppImage named for Kubetail in the folders one is
// usually kept in under home.
func appImages(home string) []string {
	var found []string
	for _, dir := range []string{"Applications", "Downloads", filepath.Join(".local", "bin")} {
		entries, _ := os.ReadDir(filepath.Join(home, dir))
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if strings.HasPrefix(name, "kubetail") && strings.HasSuffix(name, ".appimage") {
				found = append(found, filepath.Join(home, dir, e.Name()))
			}
		}
	}
	return found
}

// windowsUninstallCommand is the program and command line that remove the
// entry of the registry's Uninstall key named key. An MSI's entry is its
// product code, removed with msiexec's /x: its UninstallString's /I opens the
// repair dialog. Any other entry's, NSIS's, is its UninstallString.
func windowsUninstallCommand(key string, msi bool, uninstallString string) (program, cmdLine string, err error) {
	if msi {
		return "msiexec.exe", "msiexec.exe /x " + key, nil
	}
	s := strings.TrimSpace(uninstallString)
	if s == "" {
		return "", "", fmt.Errorf("%s has no UninstallString", key)
	}
	if rest, ok := strings.CutPrefix(s, `"`); ok {
		program, _, ok = strings.Cut(rest, `"`)
		if !ok || program == "" {
			return "", "", fmt.Errorf("%s's UninstallString %q has an unclosed quote", key, s)
		}
		return program, s, nil
	}
	program, _, _ = strings.Cut(s, " ")
	return program, s, nil
}

// Confirm asks on out and reads the answer from in: only y or yes agrees.
func Confirm(in io.Reader, out io.Writer, question string) bool {
	fmt.Fprintf(out, "%s [y/N] ", question)
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

// App is an installed copy of Kubetail, and how to remove it.
type App struct {
	// Name is what the user is shown: a path, a package or an installer.
	Name   string
	remove func() error
}

// Remove uninstalls a.
func (a App) Remove() error {
	fmt.Printf("uninstalling %s\n", a.Name)
	return a.remove()
}
