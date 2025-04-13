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
	"sync"

	evbus "github.com/asaskevich/EventBus"
	"github.com/fsnotify/fsnotify"
	zlog "github.com/rs/zerolog/log"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Represents KubeConfigWatcher
type KubeConfigWatcher struct {
	configDir  string
	kubeConfig *api.Config
	watcher    *fsnotify.Watcher
	eventbus   evbus.Bus
	mu         sync.RWMutex
}

// build custom config loading rules
func buildCustomConfigLoadingRules(configDir string) (*clientcmd.ClientConfigLoadingRules, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()

	// check if KUBECONFIG is set
	if envPath := os.Getenv(clientcmd.RecommendedConfigPathEnvVar); envPath != "" {
		rules.ExplicitPath = envPath
		return rules, nil
	}

	// otherwise, collect all < 1MB files from ~/.kube/
	files, err := os.ReadDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read .kube directory: %w", err)
	}

	var configPaths []string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		fullPath := filepath.Join(configDir, file.Name())

		info, err := file.Info()
		if err != nil {
			continue
		}
		if info.Size() < 1<<20 { // less than 1MB
			configPaths = append(configPaths, fullPath)
		}
	}

	rules.Precedence = configPaths
	return rules, nil
}

// Creates new KubeConfigWatcher instance
func NewKubeConfigWatcher() (*KubeConfigWatcher, error) {
	// Initialize kube config
	// TODO: Handle missing kube config files more gracefully
	configDir := clientcmd.RecommendedConfigDir
	rules, err := buildCustomConfigLoadingRules(configDir)
	if err != nil {
		return nil, err
	}

	// Ignore errors, as they are likely caused by invalid kubeconfig files in the .kube directory.
	kubeConfig, _ := rules.Load()
	// Initialize watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	err = watcher.Add(configDir)
	if err != nil {
		return nil, err
	}

	// Initialize
	w := &KubeConfigWatcher{
		configDir:  configDir,
		kubeConfig: kubeConfig,
		watcher:    watcher,
		eventbus:   evbus.New(),
	}

	// Start event listeners
	go w.start()

	return w, nil
}

// Get
func (w *KubeConfigWatcher) Get() *api.Config {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.kubeConfig == nil {
		return &api.Config{}
	}

	return w.kubeConfig
}

// Subscribe
func (w *KubeConfigWatcher) Subscribe(topic string, fn interface{}) {
	w.eventbus.SubscribeAsync(topic, fn, true)
}

// Unsubscribe
func (w *KubeConfigWatcher) Unsubscribe(topic string, fn interface{}) {
	w.eventbus.Unsubscribe(topic, fn)
}

// Close
func (w *KubeConfigWatcher) Close() {
	w.watcher.Close()
}

// Start
func (w *KubeConfigWatcher) start() {
	for {
		select {
		case err, ok := <-w.watcher.Errors:
			// Kill goroutine on watcher close
			if !ok {
				return
			}

			// Log error and keep listening
			zlog.Error().Err(err).Caller().Send()
		case fsEv, ok := <-w.watcher.Events:
			// Kill goroutine on watcher close
			if !ok {
				return
			}

			if fsEv.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) != 0 {
				w.mu.Lock()
				rules, err := buildCustomConfigLoadingRules(w.configDir)
				if err != nil {
					w.mu.Unlock()
					zlog.Error().Err(err).Caller().Send()
					break
				}
				oldConfig := w.kubeConfig
				kubeConfig, _ := rules.Load()
				w.kubeConfig = kubeConfig
				w.mu.Unlock()
				// Publish event
				w.eventbus.Publish("MODIFIED", oldConfig, kubeConfig)
			}
		}
	}
}
