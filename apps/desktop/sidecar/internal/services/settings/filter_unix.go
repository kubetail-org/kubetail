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

//go:build !windows

package settings

import (
	"os"
	"os/user"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"syscall"
)

// adminGroups are the groups whose members can already write anywhere
// through sudo, so a folder they can write adds no writer.
var adminGroups = func() []string {
	if runtime.GOOS == "darwin" {
		return []string{"admin", "wheel"}
	}
	return []string{"root", "wheel", "sudo", "admin"}
}()

// lookupGroupID is user.LookupGroupId, or a test's fake. groupNames caches
// its answers by gid for the sidecar's life, "" for a group it could not
// name, since a run checks every entry.
var (
	lookupGroupID = user.LookupGroupId
	groupNames    sync.Map
)

// shared reports whether the folder is writable by a group that is not an
// administrators' one: the group's other members can put a program in it.
func shared(info os.FileInfo) bool {
	if info.Mode().Perm()&0o020 == 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return !ok || !adminGroup(st.Gid)
}

// adminGroup reports whether gid is an administrators' group: gid 0, or one
// named in adminGroups. A group that cannot be looked up is not.
func adminGroup(gid uint32) bool {
	if gid == 0 {
		return true
	}
	name, ok := groupNames.Load(gid)
	if !ok {
		g, err := lookupGroupID(strconv.FormatUint(uint64(gid), 10))
		name = ""
		if err == nil {
			name = g.Name
		}
		groupNames.Store(gid, name)
	}
	return slices.Contains(adminGroups, name.(string))
}
