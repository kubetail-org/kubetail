//go:build windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"

	"github.com/Microsoft/go-winio"
)

// endpoints numbers each pipe name. A listener abandoned past closeGrace keeps
// its pipe alive, so a name reused by the next -count iteration is refused.
var endpoints atomic.Int64

// testEndpoint returns a fresh pipe name for one test.
func testEndpoint(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\kubetail-test-%d-%d-%s`, os.Getpid(), endpoints.Add(1), t.Name())
}

func dialEndpoint(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}
