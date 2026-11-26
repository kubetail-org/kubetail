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

package realpath

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fullPath is getattrlist's answer for ATTR_CMN_FULLPATH: its size, then
// where the path starts, counted from offset's own address, and the path's
// length with its closing NUL.
type fullPath struct {
	size   uint32
	offset int32
	length uint32
	path   [unix.PathMax]byte
}

// KernelPath is the path the kernel holds for the file path names: links
// followed, each component as the volume stores it, which is the path
// Seatbelt checks a file on. It is one lookup, and like a stat it needs no
// access to the file itself, so it opens no folder macOS guards.
func KernelPath(path string) (string, error) {
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return "", &os.PathError{Op: "getattrlist", Path: path, Err: err}
	}
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_FULLPATH}
	var answer fullPath
	//lint:ignore SA1019 x/sys has no libSystem wrapper for getattrlist, and the sidecar builds without cgo.
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLIST,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&answer)), unsafe.Sizeof(answer), 0, 0)
	if errno != 0 {
		return "", &os.PathError{Op: "getattrlist", Path: path, Err: errno}
	}
	start := int(unsafe.Offsetof(answer.offset)) + int(answer.offset) - int(unsafe.Offsetof(answer.path))
	end := start + int(answer.length) - 1
	if start < 0 || end < start || end > len(answer.path) { // coverage:ignore the kernel names no path longer than PathMax
		return "", &os.PathError{Op: "getattrlist", Path: path, Err: unix.ENAMETOOLONG}
	}
	return string(answer.path[start:end]), nil
}
