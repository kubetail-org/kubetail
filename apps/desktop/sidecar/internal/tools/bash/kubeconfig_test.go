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

package bash

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
)

// The run's kubeconfig is one cluster at the fixed server dialled through the
// run's proxy-url, which holds the grant's token, one user with nothing in it,
// and one context, current, named as the card names it.
func TestTheRunsKubeconfigNamesTheCardsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")

	require.NoError(t, writeKubeconfig(path, "prod-admin", 41234, "the-token"))

	cfg, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "prod-admin", cfg.CurrentContext)
	require.Len(t, cfg.Contexts, 1)
	require.Contains(t, cfg.Contexts, "prod-admin")
	ctx := cfg.Contexts["prod-admin"]
	require.Len(t, cfg.Clusters, 1)
	cluster := cfg.Clusters[ctx.Cluster]
	require.NotNil(t, cluster)
	assert.Equal(t, "http://cluster.kubetail.invalid", cluster.Server)
	assert.Equal(t, "http://kubetail:the-token@127.0.0.1:41234", cluster.ProxyURL)
	require.Len(t, cfg.AuthInfos, 1)
	user := cfg.AuthInfos[ctx.AuthInfo]
	require.NotNil(t, user)
	assert.Empty(t, user.Token)
	assert.Nil(t, user.Exec)
}

// A kubeconfig is written only where there is none: one already at the path is
// left as it is.
func TestTheRunsKubeconfigReplacesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte("planted"), 0o600))

	require.ErrorIs(t, writeKubeconfig(path, "prod", 1, "t"), fs.ErrExist)

	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "planted", string(b))
}

// The context's name is the user's kubeconfig text: it is written as data, and
// loads back unchanged whatever it holds.
func TestAContextNameIsDataInTheKubeconfig(t *testing.T) {
	name := "a \"quoted\": {{ .Name }}\nsecond line"
	path := filepath.Join(t.TempDir(), "kubeconfig")

	require.NoError(t, writeKubeconfig(path, name, 1, "t"))

	cfg, err := clientcmd.LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, name, cfg.CurrentContext)
	assert.Contains(t, cfg.Contexts, name)
}

// A context too long for the card is named as the card names it, so the
// prompts' --context with the card's text selects it.
func TestALongContextIsNamedAsTheCardNamesIt(t *testing.T) {
	name := clustercard.ContextName(kubeCluster(strings.Repeat("c", 100), ""))
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, writeKubeconfig(path, name, 1, "t"))

	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: path},
		&clientcmd.ConfigOverrides{CurrentContext: name},
	)
	rest, err := loader.ClientConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://cluster.kubetail.invalid", rest.Host)
}
