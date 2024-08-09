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

package logmetadata2

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"

	"github.com/kubetail-org/kubetail/backend/agent/internal/grpchelpers"
	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

type LogMetadataService struct {
	agentpb.UnimplementedLogMetadataServiceServer
	k8sCfg           *rest.Config
	nodeName         string
	containerLogsDir string
	testClientset    *fake.Clientset
}

// Implementation of List() in LogMetadataService
func (s *LogMetadataService) List(ctx context.Context, req *agentpb.LogMetadataListRequest) (*agentpb.LogMetadataList, error) {
	clientset := s.newK8SClientset(ctx)

	// check permission
	if err := checkPermission(ctx, clientset, req.Namespaces, "list"); err != nil {
		return nil, err
	}

	files, err := os.ReadDir(s.containerLogsDir)
	if err != nil {
		return nil, err
	}

	items := []*agentpb.LogMetadata{}

	// iterate over files
	for _, file := range files {
		// get info
		fileInfo, err := newLogMetadataFileInfo(path.Join(s.containerLogsDir, file.Name()))
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

// Implementation of Watch() in LogMetadataService
func (s *LogMetadataService) Watch(req *agentpb.LogMetadataWatchRequest, stream agentpb.LogMetadataService_WatchServer) error {
	ctx := stream.Context()
	clientset := s.newK8SClientset(ctx)

	// check permission
	if err := checkPermission(ctx, clientset, req.Namespaces, "watch"); err != nil {
		return err
	}

	return nil
}

func (s *LogMetadataService) newK8SClientset(ctx context.Context) kubernetes.Interface {
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
		panic(err)
	}

	return clientset
}

// Initialize new instance of LogMetadataService
func NewLogMetadataService(k8sCfg *rest.Config, nodeName string, containerLogsDir string) (*LogMetadataService, error) {
	s := &LogMetadataService{
		k8sCfg:           k8sCfg,
		nodeName:         nodeName,
		containerLogsDir: containerLogsDir,
	}
	return s, nil
}
