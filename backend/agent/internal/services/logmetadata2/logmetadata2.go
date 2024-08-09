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
	if err := checkPermission(ctx, clientset, req.Namespaces, "get"); err != nil {
		return nil, err
	}

	return &agentpb.LogMetadataList{}, nil
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
