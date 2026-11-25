//go:build !windows

package ipc

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The bound socket must land at 0600 regardless of the caller's umask —
// otherwise another local user on a shared box could connect during the
// window between bind and chmod. See listen_unix.go for the umask dance
// this guards.
func TestListen_IsOwnerOnly(t *testing.T) {
	for _, umask := range []int{0o022, 0o000} {
		t.Run("", func(t *testing.T) {
			prev := syscall.Umask(umask)
			defer syscall.Umask(prev)

			path := testEndpoint(t)
			ln, err := Listen(path)
			require.NoError(t, err)
			defer ln.Close()

			fi, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm(), "umask was %#o", umask)
		})
	}
}
