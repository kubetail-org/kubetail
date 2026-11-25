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

//go:build windows

package fileguard

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// A junction is a link: Go reports it as ModeIrregular, so Open reads its tag.
// mklink /J needs no privilege, where a symbolic link does.
func TestOpenRefusesAJunction(t *testing.T) {
	target := t.TempDir()
	junction := filepath.Join(t.TempDir(), "j")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput()
	require.NoError(t, err, string(out))

	_, err = Open(junction)
	var link ErrLink
	require.ErrorAs(t, err, &link)
	assert.Equal(t, target, link.Target)
}

// Either separator is plain and both answer one native path; a Git Bash drive
// path is native; plainness is judged before the conversion; a UNC or device
// path is refused.
func TestAbsRefusesAPathThatIsNotPlain(t *testing.T) {
	for p, want := range map[string]string{
		`C:/Users/x`: `C:\Users\x`,
		`C:\Users\x`: `C:\Users\x`,
		`/c/Users/x`: `C:\Users\x`,
		`C:\`:        `C:\`,
	} {
		got, err := Abs(p)
		require.NoError(t, err, p)
		assert.Equal(t, want, got, p)
	}

	for p, plain := range map[string]string{
		`/c/a/../b`: `/c/b`,
		`C:\a\..\b`: `C:\b`,
		`C:/a//b`:   `C:/a/b`,
		`C:\a\`:     `C:\a`,
	} {
		_, err := Abs(p)
		var np ErrNotPlain
		require.ErrorAs(t, err, &np, p)
		assert.Equal(t, plain, np.Plain, p)
	}

	for _, p := range []string{`\\host\share\x`, `\\?\C:\x`, `\\.\x`, `//host/share/x`} {
		_, err := Abs(p)
		assert.ErrorIs(t, err, ErrNotLocal, p)
	}
	for _, p := range []string{`\x`, `C:x`, `/tmp/x`} {
		_, err := Abs(p)
		assert.ErrorIs(t, err, ErrNotAbs, p)
	}
}

// Named compares by name, and Windows compares names without case: the data
// directory in another case is under the fence by name as well.
func TestNamedCatchesTheDataDirInAnotherCase(t *testing.T) {
	f, upper := otherCase(t)

	assert.True(t, f.Named(upper))
}

// A file that is not a reparse point has no tag; a path that cannot name a
// directory entry has none to read.
func TestReparseTag(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	tag, err := reparseTag(file)
	require.NoError(t, err)
	assert.Zero(t, tag)

	_, err = reparseTag(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
	_, err = reparseTag("C:\\a\x00b")
	assert.Error(t, err)
}

// junction is a junction to a fresh directory, made in dir.
func junction(t *testing.T, dir string) string {
	t.Helper()
	j := filepath.Join(dir, "j")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", j, t.TempDir()).CombinedOutput()
	require.NoError(t, err, string(out))
	return j
}

// withoutBackupPrivilege runs the rest of the test on a thread impersonating
// the process token minus SeBackupPrivilege. FindFirstFile opens with backup
// intent, so with the privilege enabled — as Git Bash enables it for an
// administrator — a deny ACE does not stop the listing.
func withoutBackupPrivilege(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	var proc windows.Token
	require.NoError(t, windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE, &proc))
	defer proc.Close()
	var tok windows.Token
	require.NoError(t, windows.DuplicateTokenEx(proc, windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_IMPERSONATE|windows.TOKEN_QUERY,
		nil, windows.SecurityImpersonation, windows.TokenImpersonation, &tok))
	t.Cleanup(func() { _ = tok.Close() })

	name, err := windows.UTF16PtrFromString("SeBackupPrivilege")
	require.NoError(t, err)
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0].Attributes = windows.SE_PRIVILEGE_REMOVED
	require.NoError(t, windows.LookupPrivilegeValue(nil, name, &tp.Privileges[0].Luid))
	// A token that lacks the privilege succeeds with ERROR_NOT_ALL_ASSIGNED.
	require.NoError(t, windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil))

	require.NoError(t, windows.SetThreadToken(nil, tok))
	t.Cleanup(func() { _ = windows.RevertToSelf() })
}

// A link in a directory that cannot be listed has no tag to read, so lstat
// fails rather than guess.
func TestLstatFailsWithoutTheTag(t *testing.T) {
	dir := t.TempDir()
	j := junction(t, dir)
	out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(RD)").CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() })
	withoutBackupPrivilege(t)

	_, _, err = lstat(j)
	assert.Error(t, err)
}

// A reparse point that is not a link is judged by the file behind it, never
// as a link. Its tag is a third party's no filter here answers, as a cloud
// file's is with its driver gone.
func TestAReparsePointThatIsNotALinkIsNoLink(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	p, err := windows.UTF16PtrFromString(file)
	require.NoError(t, err)
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	// REPARSE_GUID_DATA_BUFFER's header: the tag, no data, and a GUID.
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint32(buf, 0x0000_0ABC)
	buf[8] = 1
	var n uint32
	err = windows.DeviceIoControl(h, windows.FSCTL_SET_REPARSE_POINT, &buf[0], uint32(len(buf)), nil, 0, &n, nil)
	windows.CloseHandle(h)
	require.NoError(t, err)

	_, isLink, _ := lstat(file)
	assert.False(t, isLink)
}
