// Copyright 2024 Andres Morey
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

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/kubetail-org/kubetail/backend/common/config"
)

// Define a regex pattern to match the filename format
var logfileRegex = regexp.MustCompile(`^(?P<PodName>[^_]+)_(?P<Namespace>[^_]+)_(?P<ContainerName>.+)-(?P<ContainerID>[^-]+)\.log$`)

func newLogMetadataSpec(nodeName string, pathname string) (*agentpb.LogMetadataSpec, error) {
	// parse file name
	matches := logfileRegex.FindStringSubmatch(strings.TrimPrefix(pathname, "/var/log/containers/"))
	if matches == nil {
		return nil, fmt.Errorf("filename format incorrect: %s", pathname)
	}

	spec := &agentpb.LogMetadataSpec{
		NodeName:      nodeName,
		PodName:       matches[1],
		Namespace:     matches[2],
		ContainerName: matches[3],
		ContainerId:   matches[4],
	}

	return spec, nil
}

func newLogMetadataFileInfo(pathname string) (*agentpb.LogMetadataFileInfo, error) {
	// do stat
	fileInfo, err := os.Stat(pathname)
	if err != nil {
		return nil, err
	}

	// init output
	out := &agentpb.LogMetadataFileInfo{
		Size:           fileInfo.Size(),
		LastModifiedAt: timestamppb.New(fileInfo.ModTime()),
	}

	return out, nil
}

// generate new LogMetadataWatchEvent from an fsnotify event
func newLogMetadataWatchEvent(event fsnotify.Event, specMap map[string]*agentpb.LogMetadataSpec) (*agentpb.LogMetadataWatchEvent, error) {
	// init watch event
	watchEv := &agentpb.LogMetadataWatchEvent{
		Object: &agentpb.LogMetadata{
			Id:       specMap[event.Name].ContainerId,
			Spec:     specMap[event.Name],
			FileInfo: &agentpb.LogMetadataFileInfo{},
		},
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		watchEv.Type = "ADDED"
		if fileInfo, err := newLogMetadataFileInfo(event.Name); err != nil {
			return nil, err
		} else {
			watchEv.Object.FileInfo = fileInfo
		}
	case event.Op&fsnotify.Write == fsnotify.Write:
		watchEv.Type = "MODIFIED"
		if fileInfo, err := newLogMetadataFileInfo(event.Name); err != nil {
			return nil, err
		} else {
			watchEv.Object.FileInfo = fileInfo
		}
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		watchEv.Type = "DELETED"
		watchEv.Object.FileInfo = &agentpb.LogMetadataFileInfo{}
	default:
		return nil, nil
	}

	return watchEv, nil
}

// server implements the agentpb.PodLogMetadataServer interface.
type server struct {
	agentpb.UnimplementedLogMetadataServiceServer
	nodeName string
}

// implementation of FileInfoList in PodLogMetadata service
func (s *server) List(ctx context.Context, req *agentpb.LogMetadataListRequest) (*agentpb.LogMetadataList, error) {
	if len(req.Namespaces) == 0 {
		return nil, fmt.Errorf("non-empty `namespaces` required")
	}

	files, err := os.ReadDir("/var/log/containers")
	if err != nil {
		return nil, err
	}

	items := []*agentpb.LogMetadata{}

	for _, file := range files {
		// get info
		fileInfo, err := newLogMetadataFileInfo(path.Join("/var/log/containers", file.Name()))
		if err != nil {
			return nil, err
		}

		// parse file name
		matches := logfileRegex.FindStringSubmatch(file.Name())
		if matches == nil {
			return nil, fmt.Errorf("filename format incorrect: %s", file.Name())
		}

		// extract vars
		podName := matches[1]
		namespace := matches[2]
		containerName := matches[3]
		containerID := matches[4]

		// skip if namespace not in request args
		if req.Namespaces[0] != "" && !slices.Contains(req.Namespaces, namespace) {
			continue
		}

		// init item
		item := &agentpb.LogMetadata{
			Id: containerID,
			Spec: &agentpb.LogMetadataSpec{
				NodeName:      s.nodeName,
				Namespace:     namespace,
				PodName:       podName,
				ContainerName: containerName,
				ContainerId:   containerID,
			},
			FileInfo: fileInfo,
		}

		// append to list
		items = append(items, item)
	}

	return &agentpb.LogMetadataList{Items: items}, nil
}

// implementation of FileInfoWatch in PodLogMetadata service
func (s *server) Watch(req *agentpb.LogMetadataWatchRequest, stream agentpb.LogMetadataService_WatchServer) error {
	if len(req.Namespaces) == 0 {
		return fmt.Errorf("non-empty `namespaces` required")
	}

	specMap := make(map[string]*agentpb.LogMetadataSpec)

	// create new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// add current files to watcher
	err = filepath.Walk("/var/log/containers", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}

			// init spec
			spec, err := newLogMetadataSpec(s.nodeName, path)
			if err != nil {
				return err
			}

			// skip if namespace not in request args
			if req.Namespaces[0] != "" && !slices.Contains(req.Namespaces, spec.Namespace) {
				return nil
			}

			// cache spec
			specMap[target] = spec

			if err := watcher.Add(target); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// listen for new files
	if err := watcher.Add("/var/log/containers"); err != nil {
		return err
	}

	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("[%s] client disconnected\n", s.nodeName)
			return nil
		case inEv, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// handle new files
			if inEv.Op&fsnotify.Create == fsnotify.Create {
				fmt.Println(inEv)
				continue
			}

			// initialize output event
			if outEv, err := newLogMetadataWatchEvent(inEv, specMap); err != nil {
				fmt.Println(err)
			} else if outEv != nil {
				stream.Send(outEv)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}

type CLI struct {
	Addr   string `validate:"omitempty,hostname_port"`
	Config string `validate:"omitempty,file"`
}

func main() {
	var cli CLI
	var params []string

	// init cobra command
	cmd := cobra.Command{
		Use:   "kubetail-agent",
		Short: "Kubetail Backend Agent",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// validate cli flags
			return validator.New().Struct(cli)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// init viper
			v := viper.New()
			v.BindPFlag("addr", cmd.Flags().Lookup("addr"))

			// override params from cli
			for _, param := range params {
				split := strings.SplitN(param, ":", 2)
				if len(split) == 2 {
					v.Set(split[0], split[1])
				}
			}

			// init config
			cfg, err := config.NewConfig(v, cli.Config)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// configure logger
			config.ConfigureLogger(config.LoggerOptions{
				Enabled: cfg.Server.Logging.Enabled,
				Level:   cfg.Server.Logging.Level,
				Format:  cfg.Server.Logging.Format,
			})

			// init service
			s := &server{nodeName: os.Getenv("NODE_NAME")}

			// init grpc server
			grpcServer := grpc.NewServer()
			agentpb.RegisterLogMetadataServiceServer(grpcServer, s)

			// init listener
			lis, err := net.Listen("tcp", cfg.Agent.Addr)
			if err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}

			// start grpc server
			zlog.Info().Msg("Starting server on " + cfg.Agent.Addr)
			if err := grpcServer.Serve(lis); err != nil {
				zlog.Fatal().Caller().Err(err).Send()
			}
		},
	}

	// define flags
	flagset := cmd.Flags()
	flagset.SortFlags = false
	flagset.StringVarP(&cli.Config, "config", "c", "", "Path to configuration file (e.g. \"/etc/kubetail/config.yaml\")")
	flagset.StringP("addr", "a", ":50051", "Host address to bind to")
	flagset.StringArrayVarP(&params, "param", "p", []string{}, "Config params")

	// execute command
	if err := cmd.Execute(); err != nil {
		zlog.Fatal().Caller().Err(err).Send()
	}
}
