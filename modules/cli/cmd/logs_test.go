package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
	logsMock "github.com/kubetail-org/kubetail/modules/shared/logs/mock"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var testLog = logs.LogRecord{
	Timestamp: time.Date(2025, 3, 13, 11, 46, 1, 123456789, time.UTC),
	Message:   "mock log",
}

func TestRunLogs_Defaults(t *testing.T) {
	buf := &bytes.Buffer{}

	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	AddLogsCmdFlags(cmd)

	mockCm := &k8shelpersmock.MockConnectionManager{}
	mockCm.On("Shutdown", mock.Anything).Return(nil)

	mockStream := &logsMock.MockStream{}
	outCh := make(chan logs.LogRecord, 1)
	outCh <- testLog
	close(outCh)

	mockStream.On("Start", mock.Anything).Return(nil)
	mockStream.On("Records").Return((<-chan logs.LogRecord)(outCh))
	mockStream.On("Sources").Return([]logs.LogSource{})
	mockStream.On("Err").Return(nil)
	mockStream.On("Close").Return()

	factory := func(ctx context.Context, cm k8shelpers.ConnectionManager, args []string, streamOpts []logs.Option) (logs.LogStream, error) {
		return mockStream, nil
	}

	RunLogs(cmd, []string{"loggen"}, factory, mockCm)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	header := lines[0]
	assert.Contains(t, header, "MESSAGE")
	assert.Contains(t, header, "TIMESTAMP")
	assert.Contains(t, header, "\u25CB")

	logLine := lines[1]
	assert.Contains(t, logLine, testLog.Message)
	assert.Contains(t, logLine, testLog.Timestamp.Format(time.RFC3339Nano))
}
