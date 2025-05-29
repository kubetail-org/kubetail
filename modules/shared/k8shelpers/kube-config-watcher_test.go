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

package k8shelpers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Helper function to create a temporary directory with a sample kubeconfig file
func createKubeConfig(kubeconfigPath string) (*clientcmdapi.Config, error) {
	uuid := uuid.New().String()

	cluster := fmt.Sprintf("cluster-%s", uuid)
	user := fmt.Sprintf("user-%s", uuid)
	context := fmt.Sprintf("context-%s", uuid)

	// Create a new empty config
	cfg := clientcmdapi.NewConfig()

	// Populate the config
	cfg.Clusters[cluster] = &clientcmdapi.Cluster{}
	cfg.AuthInfos[user] = &clientcmdapi.AuthInfo{}
	cfg.Contexts[context] = &clientcmdapi.Context{}
	cfg.CurrentContext = context

	// Write the config to a file
	if err := clientcmd.WriteToFile(*cfg, kubeconfigPath); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Helper function to assert that two maps have the same keys
func compareMaps[K comparable, V any](t *testing.T, m1 map[K]*V, m2 map[K]*V) {
	assert.Equal(t, len(m1), len(m2))
	for k := range m1 {
		_, ok := m2[k]
		assert.True(t, ok)
	}
}

func TestKubeConfigWatcherGet(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "kube-config-watcher-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Create pathname
	kubeconfigPath := filepath.Join(tempDir, fmt.Sprintf("config-%s", uuid.New().String()))

	// Create config file
	cfgExpected, err := createKubeConfig(kubeconfigPath)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize watcher
	watcher, err := NewKubeConfigWatcher(kubeconfigPath)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	// Check config
	cfgActual := watcher.Get()
	compareMaps(t, cfgExpected.Clusters, cfgActual.Clusters)
	compareMaps(t, cfgExpected.AuthInfos, cfgActual.AuthInfos)
	compareMaps(t, cfgExpected.Contexts, cfgActual.Contexts)
	assert.Equal(t, cfgExpected.CurrentContext, cfgActual.CurrentContext)
}
