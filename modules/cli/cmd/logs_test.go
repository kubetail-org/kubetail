package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogsCmd_WithTestFlag(t *testing.T) {
	cmd := rootCmd
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"logs", "-h", "10", "--test"}) // extra arg just to satisfy minimun args constraint by Cobra

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), "ok")
}
