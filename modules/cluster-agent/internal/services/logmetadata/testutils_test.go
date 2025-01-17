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

package logmetadata

import (
	"context"
	"net"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	authv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/kubetail-org/kubetail/modules/cluster-agent/internal/server"
	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
)

// Test client
type TestClient struct {
	clusteragentpb.LogMetadataServiceClient
	grpcConn *grpc.ClientConn
}

// Close underlying grpc connection
func (tc *TestClient) Close() error {
	return tc.grpcConn.Close()
}

// Test Server
type TestServer struct {
	*grpc.Server
	cfg *config.Config
	svc *LogMetadataService
	lis *bufconn.Listener
}

// Allow pods/log actions
func (ts *TestServer) AllowSSAR(namespaces []string, verbs []string) {
	ts.svc.testClientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Cast the action to CreateAction to access the object being created
		createAction := action.(k8stesting.CreateAction)
		ssar := createAction.GetObject().(*authv1.SelfSubjectAccessReview)

		// check ssar
		if slices.Contains(verbs, ssar.Spec.ResourceAttributes.Verb) && slices.Contains(namespaces, ssar.Spec.ResourceAttributes.Namespace) {
			ssar.Status.Allowed = true
		}

		// Return the modified SelfSubjectAccessReview
		return true, ssar, nil
	})
}

// Clear actions
func (ts *TestServer) ResetClientset() {
	ts.svc.testClientset = fake.NewSimpleClientset()
}

// Initialize new TestClient instance
func (ts *TestServer) NewTestClient() *TestClient {
	// init conn
	dialerFunc := func(ctx context.Context, _ string) (net.Conn, error) {
		return ts.lis.DialContext(ctx)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialerFunc),
		grpc.WithUnaryInterceptor(grpchelpers.AuthUnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpchelpers.AuthStreamClientInterceptor),
	}

	grpcConn, err := grpc.NewClient("passthrough://bufnet", opts...)
	if err != nil {
		panic(err)
	}

	// init client
	client := clusteragentpb.NewLogMetadataServiceClient(grpcConn)

	// return test client
	return &TestClient{LogMetadataServiceClient: client, grpcConn: grpcConn}
}

// Initialize new TestServer instance
func NewTestServer(cfg *config.Config) (*TestServer, error) {
	// init service
	svc, err := NewLogMetadataService(nil, "node-name", cfg.Agent.ContainerLogsDir)
	if err != nil {
		return nil, err
	}

	// init fake clientset
	svc.testClientset = fake.NewSimpleClientset()

	// init server
	grpcServer, _ := server.NewServer(cfg)
	clusteragentpb.RegisterLogMetadataServiceServer(grpcServer, svc)

	// init listener
	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()

	// init test server
	ts := &TestServer{
		Server: grpcServer,
		svc:    svc,
		lis:    lis,
		cfg:    cfg,
	}

	return ts, nil
}

func allowSSAR(clientset *fake.Clientset, namespaces []string, verbs []string) {
	clientset.Fake.PrependReactor("create", "selfsubjectaccessreviews", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		// Cast the action to CreateAction to access the object being created
		createAction := action.(k8stesting.CreateAction)
		ssar := createAction.GetObject().(*authv1.SelfSubjectAccessReview)

		// check ssar
		if slices.Contains(verbs, ssar.Spec.ResourceAttributes.Verb) && slices.Contains(namespaces, ssar.Spec.ResourceAttributes.Namespace) {
			ssar.Status.Allowed = true
		}

		// Return the modified SelfSubjectAccessReview
		return true, ssar, nil
	})
}
