// Copyright 2024-2025 Andres Morey
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

package logrecords

import (
	"bufio"
	"context"
	"os/exec"
	"strings"

	zlog "github.com/rs/zerolog/log"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"

	"github.com/kubetail-org/kubetail/modules/cluster-agent/internal/helpers"
)

// Represents LogRecords service
type LogRecordsService struct {
	clusteragentpb.UnimplementedLogRecordsServiceServer
	k8sCfg           *rest.Config
	containerLogsDir string
	testClientset    *fake.Clientset
	shutdownCh       chan struct{}
}

// Initialize new instance of LogRecordsService
func NewLogRecordsService(k8sCfg *rest.Config, containerLogsDir string) (*LogRecordsService, error) {
	return &LogRecordsService{
		k8sCfg:           k8sCfg,
		containerLogsDir: containerLogsDir,
		shutdownCh:       make(chan struct{}),
	}, nil
}

// Initiate shutdown
func (s *LogRecordsService) Shutdown() {
	close(s.shutdownCh)
}

// Implementation of StreamForward() in LogRecordsService
func (s *LogRecordsService) StreamForward(req *clusteragentpb.LogRecordsStreamRequest, stream clusteragentpb.LogRecordsService_StreamForwardServer) error {
	logger := zlog.With().
		Str("component", "logrecords/stream-forward").
		Str("request-id", helpers.RandomString(8)).
		Logger()

	logger.Debug().Msg("new client connected")

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	clientset := s.newK8SClientset(ctx)

	// check permission
	if err := helpers.CheckPermission(ctx, clientset, []string{req.Namespace}, "list"); err != nil {
		return err
	}

	pathname, err := findLogFile(s.containerLogsDir, req.Namespace, req.PodName, req.ContainerName)
	if err != nil {
		return err
	}

	args := []string{
		"stream-forward", pathname,
		"--grep", req.Grep,
		"--follow-from", strings.ToLower(req.FollowFrom.String()),
	}

	if req.StartTime != "" {
		args = append(args, "--start-time", req.StartTime)
	}

	if req.StopTime != "" {
		args = append(args, "--stop-time", req.StopTime)
	}

	cmd := exec.CommandContext(ctx, "rgkl", args...)

	// Get a pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Set up a scanner to read stderr.
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			// Log each line from stderr.
			logger.Error().Msg("stderr: " + stderrScanner.Text())
		}
		if err := stderrScanner.Err(); err != nil {
			logger.Error().Err(err).Msg("Error reading stderr")
		}
	}()

	// Create a channel to forward full lines read from stdout.
	stdoutChan := make(chan string)

	// Spawn a goroutine that reads from stdout and sends complete lines.
	go func() {
		defer close(stdoutChan)

		for scanner.Scan() {
			stdoutChan <- scanner.Text()
		}

		if err := scanner.Err(); err != nil {
			logger.Error().Err(err).Msg("Error reading command output")
		}
	}()

	// Start command
	if err := cmd.Start(); err != nil {
		return err
	}

	// worker loop
Loop:
	for {
		select {
		case <-s.shutdownCh:
			logger.Debug().Msg("received shutdown signal")
			break Loop
		case <-ctx.Done():
			logger.Debug().Msg("client disconnected")
			break Loop
		case jsonStr, ok := <-stdoutChan:
			if !ok {
				logger.Debug().Str("json", jsonStr).Msg("stdout channel closed")
				break Loop
			}

			output := &clusteragentpb.LogRecord{}
			if err := protojson.Unmarshal([]byte(jsonStr), output); err != nil {
				logger.Error().Err(err).Send()
			}

			// write to stream
			err = stream.Send(output)
			if err != nil {
				logger.Error().Err(err).Send()
				break Loop
			}
		}
	}

	// Kill command
	cancel()

	return nil
}

// Implementation of StreamBackward() in LogRecordsService
func (s *LogRecordsService) StreamBackward(req *clusteragentpb.LogRecordsStreamRequest, stream clusteragentpb.LogRecordsService_StreamBackwardServer) error {
	logger := zlog.With().
		Str("component", "logrecords/stream-backward").
		Str("request-id", helpers.RandomString(8)).
		Logger()

	logger.Debug().Msg("new client connected")

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	clientset := s.newK8SClientset(ctx)

	// check permission
	if err := helpers.CheckPermission(ctx, clientset, []string{req.Namespace}, "list"); err != nil {
		return err
	}

	pathname, err := findLogFile(s.containerLogsDir, req.Namespace, req.PodName, req.ContainerName)
	if err != nil {
		return err
	}

	args := []string{
		"stream-backward", pathname,
		"--grep", req.Grep,
	}

	if req.StartTime != "" {
		args = append(args, "--start-time", req.StartTime)
	}

	if req.StopTime != "" {
		args = append(args, "--stop-time", req.StopTime)
	}

	cmd := exec.CommandContext(ctx, "rgkl", args...)

	// Get a pipe
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Set up a scanner to read stderr.
	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			// Log each line from stderr.
			logger.Error().Msg("stderr: " + stderrScanner.Text())
		}
		if err := stderrScanner.Err(); err != nil {
			logger.Error().Err(err).Msg("Error reading stderr")
		}
	}()

	// Create a channel to forward full lines read from stdout.
	stdoutChan := make(chan string)

	// Spawn a goroutine that reads from stdout and sends complete lines.
	go func() {
		defer close(stdoutChan)

		for scanner.Scan() {
			stdoutChan <- scanner.Text()
		}

		if err := scanner.Err(); err != nil {
			logger.Error().Err(err).Msg("Error reading command output")
		}
	}()

	// Start command
	if err := cmd.Start(); err != nil {
		return err
	}

	// worker loop
Loop:
	for {
		select {
		case <-s.shutdownCh:
			logger.Debug().Msg("received shutdown signal")
			break Loop
		case <-ctx.Done():
			logger.Debug().Msg("client disconnected")
			break Loop
		case jsonStr, ok := <-stdoutChan:
			if !ok {
				logger.Debug().Str("json", jsonStr).Msg("stdout channel closed")
				break Loop
			}

			output := &clusteragentpb.LogRecord{}
			if err := protojson.Unmarshal([]byte(jsonStr), output); err != nil {
				logger.Error().Err(err).Send()
			}

			// write to stream
			err = stream.Send(output)
			if err != nil {
				logger.Error().Err(err).Send()
				break Loop
			}
		}
	}

	// Kill command
	cancel()

	return nil
}

// Initialize new kubernetes clientset
func (s *LogRecordsService) newK8SClientset(ctx context.Context) kubernetes.Interface {
	if s.testClientset != nil {
		return s.testClientset
	}

	// copy config
	cfg := rest.CopyConfig(s.k8sCfg)

	// get token from context
	token, ok := ctx.Value(grpchelpers.K8STokenCtxKey).(string)
	if ok {
		cfg.BearerToken = token
		cfg.BearerTokenFile = ""
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	return clientset
}
