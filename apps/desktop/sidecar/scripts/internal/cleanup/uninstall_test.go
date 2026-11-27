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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
)

// On macOS the release's paths add WKWebView's folders under the bundle id
// to the layout's top folders.
func TestTheMacOSUninstallPathsAddTheWebKitFolders(t *testing.T) {
	r := testRoots()
	l := layout.Of(layout.MacOS, false, r)
	lib := filepath.Join(r.Home, "Library")

	assert.Equal(t, []string{
		filepath.Join(r.Cache, "Kubetail"),
		filepath.Join(lib, "Logs", "Kubetail"),
		filepath.Join(r.LocalData, "Kubetail"),
		filepath.Join(lib, "WebKit", "com.kubetail.desktop"),
		filepath.Join(lib, "Caches", "com.kubetail.desktop"),
		filepath.Join(lib, "HTTPStorages", "com.kubetail.desktop"),
		filepath.Join(lib, "HTTPStorages", "com.kubetail.desktop.binarycookies"),
		filepath.Join(lib, "Saved Application State", "com.kubetail.desktop.savedState"),
	}, UninstallPaths(layout.MacOS, l, r.Home, nil))
}

func TestTheUninstallPathsEndWithTheRuntimeDirs(t *testing.T) {
	r := testRoots()
	runtime := filepath.FromSlash("/run/user/501/kubetail")
	paths := UninstallPaths(layout.Linux, layout.Of(layout.Linux, false, r), r.Home, []string{runtime})

	assert.Equal(t, append(TopDirs(layout.Of(layout.Linux, false, r)), runtime), paths)
}

// A dev build's folders are clean-desktop-data's: the release's paths never
// name one.
func TestTheUninstallPathsNameNoDevFolder(t *testing.T) {
	r := testRoots()
	for _, platform := range []layout.Platform{layout.Linux, layout.MacOS, layout.Windows} {
		for _, path := range UninstallPaths(platform, layout.Of(platform, false, r), r.Home, nil) {
			assert.NotContains(t, strings.ToLower(path), "-dev", "%s: %s", platform, path)
		}
	}
}

func TestExistingKeepsThePathsThatExist(t *testing.T) {
	top := t.TempDir()
	dir := filepath.Join(top, "dir")
	file := filepath.Join(top, "file")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	found, err := Existing([]string{dir, filepath.Join(top, "missing"), file})
	require.NoError(t, err)
	assert.Equal(t, []string{dir, file}, found)
}

func TestTheMacOSAppIsInEitherApplicationsFolder(t *testing.T) {
	home := filepath.FromSlash("/Users/u")
	assert.Equal(t, []string{
		filepath.Join("/Applications", "Kubetail.app"),
		filepath.Join(home, "Applications", "Kubetail.app"),
	}, macOSApps(home))
}

func TestTheLinuxPackageIsRemovedThroughItsManager(t *testing.T) {
	assert.Equal(t, []string{"sudo", "apt-get", "remove", "-y", "kubetail-desktop"}, linuxRemoveCommand("apt-get", false))
	assert.Equal(t, []string{"dnf", "remove", "-y", "kubetail-desktop"}, linuxRemoveCommand("dnf", true))
}

func TestAppImagesAreFoundInTheUsualFolders(t *testing.T) {
	home := t.TempDir()
	for _, path := range []string{
		filepath.Join("Downloads", "kubetail-desktop_0.1.0_amd64.AppImage"),
		filepath.Join(".local", "bin", "Kubetail.AppImage"),
		filepath.Join("Downloads", "other.AppImage"),
		filepath.Join("Downloads", "kubetail-desktop_0.1.0_amd64.deb"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, filepath.Dir(path)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(home, path), nil, 0o600))
	}

	assert.ElementsMatch(t, []string{
		filepath.Join(home, "Downloads", "kubetail-desktop_0.1.0_amd64.AppImage"),
		filepath.Join(home, ".local", "bin", "Kubetail.AppImage"),
	}, appImages(home))
}

func TestTheWindowsUninstallCommand(t *testing.T) {
	cases := []struct {
		name            string
		msi             bool
		uninstallString string
		program         string
		cmdLine         string
	}{
		{"an MSI is removed by its product code", true, "MsiExec.exe /I{ABC}", "msiexec.exe", "msiexec.exe /x {ABC}"},
		{"a quoted program", false, `"C:\Program Files\Kubetail\uninstall.exe" /S`, `C:\Program Files\Kubetail\uninstall.exe`, `"C:\Program Files\Kubetail\uninstall.exe" /S`},
		{"a bare program", false, `C:\Kubetail\uninstall.exe`, `C:\Kubetail\uninstall.exe`, `C:\Kubetail\uninstall.exe`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			program, cmdLine, err := windowsUninstallCommand("{ABC}", c.msi, c.uninstallString)
			require.NoError(t, err)
			assert.Equal(t, c.program, program)
			assert.Equal(t, c.cmdLine, cmdLine)
		})
	}
}

func TestAWindowsEntryWithNoUsableCommandIsAnError(t *testing.T) {
	_, _, err := windowsUninstallCommand("Kubetail", false, " ")
	assert.ErrorContains(t, err, "no UninstallString")
	_, _, err = windowsUninstallCommand("Kubetail", false, `"C:\uninstall.exe`)
	assert.ErrorContains(t, err, "unclosed quote")
}

func TestConfirmAgreesOnlyToYes(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "YES\n": true, " yes \n": true, "\n": false, "n\n": false, "": false, "yess\n": false} {
		var out strings.Builder
		assert.Equal(t, want, Confirm(strings.NewReader(answer), &out, "Continue?"), "%q", answer)
		assert.Equal(t, "Continue? [y/N] ", out.String())
	}
}
