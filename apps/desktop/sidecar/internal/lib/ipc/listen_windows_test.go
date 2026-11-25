//go:build windows

package ipc

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// Guards the SDDL string that gates pipe access. The realistic regression
// is an edit that broadens the trustee (e.g. WD = Everyone, AU = Authenticated
// Users) or drops the DACL entirely (NULL DACL grants Everyone full access).
//
// We parse the string, assert the DACL is present (not NULL), and round-trip
// it back to SDDL — any structural change to the constant forces the test to
// be updated, which means a human looks at the new policy.
func TestOwnerOnlyDACL_ParsesAndIsRestrictive(t *testing.T) {
	sd, err := windows.SecurityDescriptorFromString(ownerOnlyDACL)
	require.NoError(t, err, "SecurityDescriptorFromString(%q)", ownerOnlyDACL)

	dacl, _, err := sd.DACL()
	require.NoError(t, err)
	require.NotNil(t, dacl, "DACL is NULL — that grants Everyone full access")

	assert.Equal(t, ownerOnlyDACL, sd.String(), "SDDL round-trip mismatch")
}

// hungListener stands in for a go-winio listener wedged by the deadlock
// pipeListener guards: its Close never returns on its own.
type hungListener struct {
	net.Listener
	unblock chan struct{}
}

func (l hungListener) Close() error {
	<-l.unblock
	return nil
}

// Shutdown must not inherit a Close that never returns.
func TestPipeListenerClose_GivesUpOnAWedgedListener(t *testing.T) {
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })

	ln := &pipeListener{
		Listener:   hungListener{unblock: unblock},
		closeGrace: time.Millisecond,
	}
	assert.NoError(t, ln.Close())
}
