//go:build !windows

package ipc

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// testEndpoint returns a fresh socket path for one test.
//
// Not t.TempDir(): it embeds the test's name, and a socket path over
// sun_path — 104 bytes on macOS — fails to bind with EINVAL.
func testEndpoint(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func dialEndpoint(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}
