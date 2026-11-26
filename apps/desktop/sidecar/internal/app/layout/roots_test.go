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

package layout

import (
	"maps"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type env map[string]string

func (e env) get(name string) string { return e[name] }

func TestLinuxRootsAreXDGsElseUnderTheHome(t *testing.T) {
	roots, err := RootsFromEnv(Linux, env{"HOME": "/home/x"}.get)
	require.NoError(t, err)
	assert.Equal(t, Roots{
		Config:    filepath.FromSlash("/home/x/.config"),
		LocalData: filepath.FromSlash("/home/x/.local/share"),
		Cache:     filepath.FromSlash("/home/x/.cache"),
		Home:      "/home/x",
		State:     filepath.FromSlash("/home/x/.local/state"),
	}, roots)

	roots, err = RootsFromEnv(Linux, env{
		"HOME":            "/home/x",
		"XDG_CONFIG_HOME": "/xdg/config",
		"XDG_DATA_HOME":   "/xdg/data",
		"XDG_CACHE_HOME":  "/xdg/cache",
		"XDG_STATE_HOME":  "/xdg/state",
	}.get)
	require.NoError(t, err)
	assert.Equal(t, Roots{Config: "/xdg/config", LocalData: "/xdg/data", Cache: "/xdg/cache", Home: "/home/x", State: "/xdg/state"}, roots)
}

// The XDG spec ignores a relative base directory, as the host does.
func TestARelativeXDGRootIsIgnored(t *testing.T) {
	roots, err := RootsFromEnv(Linux, env{"HOME": "/home/x", "XDG_DATA_HOME": "data"}.get)
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/home/x/.local/share"), roots.LocalData)
}

func TestMacOSRootsAreUnderTheHome(t *testing.T) {
	roots, err := RootsFromEnv(MacOS, env{"HOME": "/Users/x", "XDG_DATA_HOME": "/xdg/data"}.get)
	require.NoError(t, err)
	support := filepath.FromSlash("/Users/x/Library/Application Support")
	assert.Equal(t, Roots{Config: support, LocalData: support, Cache: filepath.FromSlash("/Users/x/Library/Caches"), Home: "/Users/x"}, roots)
}

func TestWindowsRootsAreTheProfileFolders(t *testing.T) {
	roots, err := RootsFromEnv(Windows, env{
		"LOCALAPPDATA": `C:\Users\x\AppData\Local`,
		"APPDATA":      `C:\Users\x\AppData\Roaming`,
		"USERPROFILE":  `C:\Users\x`,
	}.get)
	require.NoError(t, err)
	assert.Equal(t, Roots{
		Config:    `C:\Users\x\AppData\Roaming`,
		LocalData: `C:\Users\x\AppData\Local`,
		Cache:     `C:\Users\x\AppData\Local`,
		Home:      `C:\Users\x`,
	}, roots)
}

// With no roaming root there is no config directory, never a failed launch.
func TestWindowsWithNoRoamingRootHasNoConfigRoot(t *testing.T) {
	roots, err := RootsFromEnv(Windows, env{"LOCALAPPDATA": `C:\Users\x\AppData\Local`, "APPDATA": `Roaming`}.get)
	require.NoError(t, err)
	assert.Empty(t, roots.Config)
}

func TestARootThatCannotBeReadIsAnError(t *testing.T) {
	for _, c := range []struct {
		platform Platform
		env      env
		name     string
	}{
		{Linux, env{}, "HOME"},
		{MacOS, env{}, "HOME"},
		// A relative home would place every folder under the launcher's
		// working directory.
		{Linux, env{"HOME": "home/x"}, "HOME"},
		{MacOS, env{"HOME": "home/x"}, "HOME"},
		{Windows, env{"APPDATA": `C:\Roaming`}, "LOCALAPPDATA"},
		{Windows, env{"LOCALAPPDATA": `AppData\Local`}, "LOCALAPPDATA"},
	} {
		_, err := RootsFromEnv(c.platform, c.env.get)
		assert.ErrorContains(t, err, c.name, "%s %v", c.platform, c.env)
	}
}

// On Unix a `..` after a link names the link target's parent, while the
// fences take it out by name, so a root holding one is refused, named by the
// variable it came from.
func TestAUnixRootHoldingDotDotIsRefused(t *testing.T) {
	for _, c := range []struct {
		platform Platform
		env      env
		name     string
	}{
		{Linux, env{"HOME": "/home/x/link/.."}, "HOME"},
		{Linux, env{"HOME": "/home/x", "XDG_CONFIG_HOME": "/x/../config"}, "XDG_CONFIG_HOME"},
		{Linux, env{"HOME": "/home/x", "XDG_DATA_HOME": "/x/../data"}, "XDG_DATA_HOME"},
		{Linux, env{"HOME": "/home/x", "XDG_CACHE_HOME": "/x/../cache"}, "XDG_CACHE_HOME"},
		{Linux, env{"HOME": "/home/x", "XDG_STATE_HOME": "/x/../state"}, "XDG_STATE_HOME"},
		{MacOS, env{"HOME": "/Users/x/link/.."}, "HOME"},
	} {
		_, err := RootsFromEnv(c.platform, c.env.get)
		assert.ErrorContains(t, err, c.name, "%s %v", c.platform, c.env)
	}
}

// As on the host, a Linux HOME matters only to a root that falls back to it:
// with every XDG variable set, one unset or holding `..` places nothing.
func TestLinuxReadsHomeOnlyForARootUnderIt(t *testing.T) {
	xdg := env{
		"XDG_CONFIG_HOME": "/xdg/config",
		"XDG_DATA_HOME":   "/xdg/data",
		"XDG_CACHE_HOME":  "/xdg/cache",
		"XDG_STATE_HOME":  "/xdg/state",
	}
	for _, home := range []string{"", "home/x", "/home/x/link/.."} {
		e := env{"HOME": home}
		maps.Copy(e, xdg)
		roots, err := RootsFromEnv(Linux, e.get)
		require.NoError(t, err, "HOME=%q", home)
		assert.Equal(t, "/xdg/state", roots.State)

		delete(e, "XDG_CACHE_HOME")
		_, err = RootsFromEnv(Linux, e.get)
		assert.ErrorContains(t, err, "HOME", "HOME=%q with no XDG_CACHE_HOME", home)
	}
}

// A name that begins with dots is not `..`.
func TestARootNamedWithDotsStands(t *testing.T) {
	roots, err := RootsFromEnv(Linux, env{"HOME": "/home/..x"}.get)
	require.NoError(t, err)
	assert.Equal(t, "/home/..x", roots.Home)
}

// Windows reads `..` by name itself.
func TestAWindowsRootHoldingDotDotStands(t *testing.T) {
	roots, err := RootsFromEnv(Windows, env{"LOCALAPPDATA": `C:\Users\x\link\..\Local`}.get)
	require.NoError(t, err)
	assert.Equal(t, `C:\Users\x\link\..\Local`, roots.LocalData)
}
