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

package app

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/app/layout"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Named is Kubetail's folders as the command line names them, unresolved.
type Named struct {
	Platform layout.Platform
	Layout   layout.Layout
	// Data, Cache and Runtime are the layout's, or a debug build's moved ones.
	Data, Cache, Runtime string
	// Profile is the user's home, which on Windows is the profile every
	// layout folder must be inside.
	Profile string
}

// Folders is where Kubetail keeps its files, every path resolved.
type Folders struct {
	Layout               layout.Layout
	Data, Cache, Runtime string
	// Links is each link followed to reach them.
	Links []realpath.Link
}

// ResolveFolders follows the links of every folder n names, once, checks the
// resolved layout, and only then makes the sidecar's own folders where they
// lead. Nothing after it reads an unresolved path. A refused layout is left as
// it was: no folder made, no mode changed.
func ResolveFolders(n Named) (Folders, error) {
	for _, d := range []struct{ name, path string }{
		{dataDirectory, n.Data},
		{cacheDirectory, n.Cache},
		{runtimeDirectory, n.Runtime},
	} {
		if !filepath.IsAbs(d.path) {
			return Folders{}, fmt.Errorf("%s must be an absolute path, not %q", d.name, d.path)
		}
	}
	f, err := follow(n)
	if err != nil {
		return Folders{}, err
	}
	if err := checkLayout(n.Platform, f); err != nil {
		return Folders{}, err
	}
	if n.Platform == layout.Windows {
		if err := checkInProfile(n, f.Layout); err != nil {
			return Folders{}, err
		}
	}
	return f, makeFolders(f)
}

// follow is n with every folder's links followed, and the links met.
func follow(n Named) (Folders, error) {
	resolved, links, err := layout.Resolve(n.Layout)
	if err != nil {
		return Folders{}, err
	}
	resolveRuntime := realpath.Followed
	if n.Platform != layout.Windows {
		resolveRuntime = layout.ResolveRuntime
	}
	f := Folders{Layout: resolved}
	for _, d := range []struct {
		dst     *string
		path    string
		resolve func(string) (string, []realpath.Link, error)
	}{
		{&f.Data, n.Data, realpath.Followed},
		{&f.Cache, n.Cache, realpath.Followed},
		{&f.Runtime, n.Runtime, resolveRuntime},
	} {
		var met []realpath.Link
		if *d.dst, met, err = d.resolve(d.path); err != nil {
			return Folders{}, err
		}
		links = append(links, met...)
	}
	for _, link := range links {
		if !slices.Contains(f.Links, link) {
			f.Links = append(f.Links, link)
		}
	}
	return f, nil
}

// makeFolders makes the sidecar's own folders. The layout's data and cache
// are made owner-only with the Kubetail folder above them; a debug build's
// moved folder, like the runtime folder, only when it is missing, since it
// may be a folder the developer shares. A moved folder is the layout's own
// when the two are one folder as files.
func makeFolders(f Folders) error {
	for _, d := range []struct{ name, path, layouts string }{
		{dataDirectory, f.Data, f.Layout.Data},
		{cacheDirectory, f.Cache, f.Layout.Cache},
		{runtimeDirectory, f.Runtime, ""},
	} {
		var err error
		if d.layouts != "" && realpath.Same(d.path, d.layouts) {
			err = layout.MakeOwnerOnly(f.Layout, d.layouts)
		} else {
			err = os.MkdirAll(d.path, 0o700)
		}
		if err != nil {
			return fmt.Errorf("make %s: %w", d.name, err)
		}
	}
	return nil
}

// The names of Kubetail's folders, as a refusal names them.
const (
	appFolder             = "the app folder"
	appFolderOnConfigRoot = "the app folder on the config root"
	dataDirectory         = "the data directory"
	configDirectory       = "the config directory"
	stateDirectory        = "the state directory"
	cacheDirectory        = "the cache directory"
	logDirectory          = "the log directory"
	runtimeDirectory      = "the runtime directory"
)

// kind is one of Kubetail's folders: its name and its resolved path.
type kind struct {
	name, path string
}

// allowedInside is, per platform, the kinds each holder may hold. Allowed is
// not required: a kind linked out of its holder is fine.
var allowedInside = map[layout.Platform]map[string][]string{
	layout.Linux: {
		stateDirectory: {logDirectory},
	},
	layout.MacOS: {
		appFolder: {dataDirectory, configDirectory, stateDirectory},
	},
	layout.Windows: {
		appFolder:             {dataDirectory, stateDirectory, cacheDirectory, runtimeDirectory, logDirectory},
		stateDirectory:        {logDirectory},
		appFolderOnConfigRoot: {configDirectory},
	},
}

// checkLayout refuses two of f's folders that are one folder, or one inside
// another where the platform's layout does not put it, compared as files.
// Windows' app folder and the one on the config root may be one folder, and
// are then one holder.
func checkLayout(platform layout.Platform, f Folders) error {
	holds := maps.Clone(allowedInside[platform])
	appOnConfig := f.Layout.AppOnConfigRoot
	if platform == layout.Windows && appOnConfig != "" && realpath.Relation(f.Layout.App, appOnConfig) == realpath.SameFolder {
		holds[appFolder] = slices.Concat(holds[appFolder], holds[appFolderOnConfigRoot])
		appOnConfig = ""
	}
	kinds := slices.DeleteFunc([]kind{
		{appFolder, f.Layout.App},
		{appFolderOnConfigRoot, appOnConfig},
		{dataDirectory, f.Data},
		{configDirectory, f.Layout.Config},
		{stateDirectory, f.Layout.State},
		{cacheDirectory, f.Cache},
		{logDirectory, f.Layout.Logs},
		{runtimeDirectory, f.Runtime},
	}, func(k kind) bool { return k.path == "" })
	for i, a := range kinds {
		for _, b := range kinds[i+1:] {
			if realpath.Relation(a.path, b.path) == realpath.SameFolder {
				return fmt.Errorf("%s and %s are both %s; give each its own folder", a.name, b.name, a.path)
			}
		}
	}
	for _, inner := range kinds {
		for _, outer := range kinds {
			if realpath.Relation(inner.path, outer.path) != realpath.Inside || slices.Contains(holds[outer.name], inner.name) {
				continue
			}
			return fmt.Errorf("%s %s is inside %s %s", inner.name, inner.path, outer.name, outer.path)
		}
	}
	return nil
}

// checkInProfile refuses a Windows layout folder a link leads out of the user
// profile, or onto it, whose ACL is what keeps the folder private. A debug
// build's moved folder is the developer's own, and is not checked.
func checkInProfile(n Named, resolved layout.Layout) error {
	if n.Profile == "" {
		return errors.New("the user profile is unknown, and Kubetail keeps its folders inside it")
	}
	profile, _, err := realpath.Followed(n.Profile)
	if err != nil {
		return err
	}
	named := n.Layout
	for _, dir := range []struct{ named, resolved string }{
		{named.App, resolved.App},
		{named.AppOnConfigRoot, resolved.AppOnConfigRoot},
		{named.Data, resolved.Data},
		{named.Config, resolved.Config},
		{named.State, resolved.State},
		{named.Cache, resolved.Cache},
		{named.Logs, resolved.Logs},
		{named.Runtime, resolved.Runtime},
	} {
		// A folder no link leads elsewhere is where Windows names it, which
		// an administrator may redirect off the profile.
		followedALink := dir.resolved != filepath.Clean(dir.named)
		if dir.resolved != "" && followedALink && realpath.Relation(dir.resolved, profile) != realpath.Inside {
			return fmt.Errorf("%s leads to %s, outside your profile; keep it inside %s", dir.named, dir.resolved, n.Profile)
		}
	}
	return nil
}
