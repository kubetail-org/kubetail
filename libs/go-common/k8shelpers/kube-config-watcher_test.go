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

package k8shelpers

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Helper function to create a unique pathname
func generateUniquePathname(dirname string) string {
	return filepath.Join(dirname, fmt.Sprintf("config-%s", uuid.New().String()))
}

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

// Helper function to merge two maps
func mergeMaps[K comparable, V any](a, b map[K]V) map[K]V {
	out := make(map[K]V, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
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

	t.Run("single file", func(t *testing.T) {
		// Create pathname
		kubeconfigPath := generateUniquePathname(tempDir)

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
	})

	t.Run("multiple files", func(t *testing.T) {
		// Create pathnames
		p1 := generateUniquePathname(tempDir)
		p2 := generateUniquePathname(tempDir)

		// Create config files
		cfg1, err := createKubeConfig(p1)
		require.NoError(t, err)
		cfg2, err := createKubeConfig(p2)
		require.NoError(t, err)

		// Set environment
		sep := string(os.PathListSeparator)
		t.Setenv(clientcmd.RecommendedConfigPathEnvVar, fmt.Sprintf("%s%s%s", p1, sep, p2))

		// Init watcher
		watcher, err := NewKubeConfigWatcher("")
		if err != nil {
			t.Fatal(err)
		}
		defer watcher.Close()

		// Check config
		cfgActual := watcher.Get()

		expectedClusters := mergeMaps(cfg1.Clusters, cfg2.Clusters)
		compareMaps(t, expectedClusters, cfgActual.Clusters)

		expectedAuthInfos := mergeMaps(cfg1.AuthInfos, cfg2.AuthInfos)
		compareMaps(t, expectedAuthInfos, cfgActual.AuthInfos)

		expectedContexts := mergeMaps(cfg1.Contexts, cfg2.Contexts)
		compareMaps(t, expectedContexts, cfgActual.Contexts)

		assert.Equal(t, cfg1.CurrentContext, cfgActual.CurrentContext)
	})
}

func TestKubeConfigWatcherSubscribeModified(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "kube-config-watcher-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Create pathnames
	p1 := generateUniquePathname(tempDir)
	p2 := generateUniquePathname(tempDir)

	// Create config files
	cfg1, err := createKubeConfig(p1)
	require.NoError(t, err)
	_, err = createKubeConfig(p2)
	require.NoError(t, err)

	// Set environment
	sep := string(os.PathListSeparator)
	t.Setenv(clientcmd.RecommendedConfigPathEnvVar, fmt.Sprintf("%s%s%s", p1, sep, p2))

	// Initialize watcher
	watcher, err := NewKubeConfigWatcher("")
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	var (
		wg        sync.WaitGroup
		cfgActual *clientcmdapi.Config
	)

	// Subscribe to changes
	wg.Add(1)
	watcher.Subscribe(func(newCfg *clientcmdapi.Config) {
		defer wg.Done()
		cfgActual = newCfg
	})

	// Modify one of the files
	cfg2, err := createKubeConfig(p2)
	require.NoError(t, err)

	wg.Wait()

	// Check new config
	expectedClusters := mergeMaps(cfg1.Clusters, cfg2.Clusters)
	compareMaps(t, expectedClusters, cfgActual.Clusters)

	expectedAuthInfos := mergeMaps(cfg1.AuthInfos, cfg2.AuthInfos)
	compareMaps(t, expectedAuthInfos, cfgActual.AuthInfos)

	expectedContexts := mergeMaps(cfg1.Contexts, cfg2.Contexts)
	compareMaps(t, expectedContexts, cfgActual.Contexts)

	assert.Equal(t, cfg1.CurrentContext, cfgActual.CurrentContext)
}

func TestKubeConfigWatcher_FileNotFound(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "kube-config-watcher-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir) // Clean up after test

	// Define non-existent path
	nonExistentPath := filepath.Join(tempDir, "non-existent-config")

	// Initialize watcher
	_, err = NewKubeConfigWatcher(nonExistentPath)

	// Assert error
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("kubeconfig file not found at '%s'", nonExistentPath))
	assert.Contains(t, err.Error(), "use the '--kubeconfig' flag")
	assert.Contains(t, err.Error(), "use the '--in-cluster' flag")
}
