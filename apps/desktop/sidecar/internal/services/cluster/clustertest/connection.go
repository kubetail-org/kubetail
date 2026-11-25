// Copyright 2024 The Kubetail Authors
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

package clustertest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
)

// Connection is a connection to the API server at host, built as the pool builds one,
// so its Retire closes Done for its holders. It trusts any certificate, as an
// httptest TLS server needs.
func Connection(t testing.TB, host string) *cluster.Connection {
	t.Helper()
	conn, err := kubeconn.NewConnection(&rest.Config{Host: host, TLSClientConfig: rest.TLSClientConfig{Insecure: true}})
	require.NoError(t, err)
	return conn
}
