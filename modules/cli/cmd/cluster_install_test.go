package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClusterInstallCmd_WithTestFlag(t *testing.T) {
	cmd := rootCmd
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"cluster", "install", "--test"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, buf.String(), "ok")
}
