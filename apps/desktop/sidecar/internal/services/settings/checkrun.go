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

package settings

import (
	"os"
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// RunCheck is the checks over a run's PATH folders, against its own open
// folders and the paths it never opens, since the zones can change after the
// setting was saved: a path the denied-always list gained, a chmod, a
// narrower open list.
type RunCheck struct {
	z zones
	// checkFolder is the package's checkFolder, or a test's stand-in.
	checkFolder func(path string, write bool, z zones, pathNoWrite []string, stored bool) error
}

// NewRunCheck resolves open, never and home's broad folders once for every
// folder of a run.
func NewRunCheck(open sandbox.FilePolicy, never []string, home string) RunCheck {
	return RunCheck{z: zonesOf(Zones{Never: never, Open: open, Home: home}), checkFolder: checkFolder}
}

// CheckedFolder is one folder of a run's PATH as the checks judged it.
type CheckedFolder struct {
	PathFolder
	// Target is the folder Path resolves to, canonical; "" when it does not
	// resolve to one a run may search.
	Target   string
	Searched bool
	// Open is whether every run reads Target already, so searching it needs
	// no Read rule.
	Open bool
	// Shared is whether a group other than an administrators' one can add
	// programs to Target.
	Shared bool
	// Reason is why the folder is left out, in the user's words.
	Reason string
	// Addable is whether typing Target into the setting would include it: a
	// shell folder left out only because no run reads it or it is shared.
	Addable bool

	info os.FileInfo // Target's, which another folder is compared with
}

// same reports whether f and o resolved to one folder, compared as files: a
// folder that folds names, or two mounts of one folder, is one folder.
func (f CheckedFolder) same(o CheckedFolder) bool {
	return f.info != nil && o.info != nil && os.SameFile(f.info, o.info)
}

// Folders checks each of folders in order. A shell folder is searched only
// when every run reads it already and it is not shared, so a startup file
// opens nothing. A typed folder is the user's read grant, held to a folder
// grant's checks. A folder already searched is not searched again. Settings'
// list and a run both read this, so they answer alike. It reads the disk.
func (c RunCheck) Folders(folders []PathFolder) []CheckedFolder {
	out := make([]CheckedFolder, len(folders))
	for i, f := range folders {
		out[i] = c.folder(f)
	}
	// listed reports whether list holds f's folder in an entry match accepts.
	listed := func(list []CheckedFolder, f CheckedFolder, match func(CheckedFolder) bool) bool {
		return slices.ContainsFunc(list, func(o CheckedFolder) bool { return match(o) && o.same(f) })
	}
	searched := func(o CheckedFolder) bool { return o.Searched }
	typedAndSearched := func(o CheckedFolder) bool { return o.Origin == OriginTyped && o.Searched }
	addable := func(o CheckedFolder) bool { return o.Addable }
	for i := range out {
		f := &out[i]
		switch {
		case f.Searched && listed(out[:i], *f, searched):
			f.Searched, f.Reason = false, "Searched earlier in the list."
		case f.Addable && listed(out, *f, typedAndSearched):
			f.Reason, f.Addable = "Searched where you typed it.", false
		case f.Addable && listed(out[:i], *f, addable):
			// One Add per folder.
			f.Addable = false
		}
	}
	return out
}

// folder is f checked alone.
func (c RunCheck) folder(f PathFolder) CheckedFolder {
	out := CheckedFolder{PathFolder: f}
	if f.Origin == OriginTyped {
		if err := c.checkFolder(f.Path, false, c.z, nil, false); err != nil {
			out.Reason = err.Error()
			return out
		}
	}
	d, rule := filterDir(f.Path, c.z)
	if rule != "" {
		out.Reason = filterReasons[rule]
		return out
	}
	out.Target, out.Shared, out.Open, out.info = d.Target, d.Shared, c.z.open(d.Target), d.info
	switch {
	case f.Origin == OriginTyped:
		out.Searched = true
	case !out.Open:
		out.Reason = "No sandboxed command reads this folder; type it into your PATH to include it."
		out.Addable = c.typeable(d.Target)
	case out.Shared:
		out.Reason = "Others in its group can add programs to it; type it into your PATH to include it."
		out.Addable = c.typeable(d.Target)
	default:
		out.Searched = true
	}
	return out
}

// typeable is whether target, typed into the setting, would be searched: it
// passes the grammar and a typed folder's check.
func (c RunCheck) typeable(target string) bool {
	return CheckPath(target) == nil && c.checkFolder(target, false, c.z, nil, false) == nil
}

// filterReasons is each rule of filterDir in the user's words.
var filterReasons = map[string]string{
	"empty":     "An empty folder is never searched.",
	"relative":  "Not a full path, so it would be searched from the command's own folder.",
	"missing":   "This folder does not exist.",
	"world":     "Anyone on this machine can add programs to it.",
	"never":     "This folder is never readable in the sandbox.",
	"broad":     "It holds too much to search; name a folder inside it.",
	"fixed":     "Every sandboxed command has its own copy of it.",
	"separator": "Its name holds the PATH separator.",
	"project":   "A project's node_modules folder holds whatever its last install fetched.",
}
