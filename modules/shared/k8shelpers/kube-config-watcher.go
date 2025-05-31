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
	"os"
	"path/filepath"
	"strings"
	"sync"

	evbus "github.com/asaskevich/EventBus"
	"github.com/fsnotify/fsnotify"
	zlog "github.com/rs/zerolog/log"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const HOMEPATH_TILDE = "~"

// Represents KubeConfigWatcher
type KubeConfigWatcher struct {
	kubeconfigPath string
	kubeConfig     *api.Config
	watcher        *fsnotify.Watcher
	eventbus       evbus.Bus
	mu             sync.RWMutex
}

// Creates new KubeConfigWatcher instance
func NewKubeConfigWatcher(kubeconfigPath string) (*KubeConfigWatcher, error) {
	// Initialize kube config
	// TODO: Handle missing kube config files more gracefully
	var kubeConfig *api.Config
	var err error

	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.RecommendedHomeFile
	} else if strings.HasPrefix(kubeconfigPath, HOMEPATH_TILDE) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			zlog.Error().Msg("Kubeconfig path is corrupted. Please provide a valid path")
			return nil, err
		}
		kubeconfigPath = filepath.Join(homeDir, kubeconfigPath[2:])
	}

	kubeConfig, err = clientcmd.LoadFromFile(kubeconfigPath)

	if err != nil {
		zlog.Error().Msg("Kubeconfig is corrupted or missing. Please provide a valid kubeconfig.")
		return nil, err
	}

	// Initialize watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	err = watcher.Add(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	// Initialize
	w := &KubeConfigWatcher{
		kubeconfigPath: kubeconfigPath,
		kubeConfig:     kubeConfig,
		watcher:        watcher,
		eventbus:       evbus.New(),
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
func (w *KubeConfigWatcher) Subscribe(topic string, fn any) {
	w.eventbus.SubscribeAsync(topic, fn, true)
}

// Unsubscribe
func (w *KubeConfigWatcher) Unsubscribe(topic string, fn any) {
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

			// Handle fsnotify events
			switch {
			case fsEv.Op&fsnotify.Create == fsnotify.Create:
				// Load config
				w.mu.Lock()
				kubeConfig, err := clientcmd.LoadFromFile(w.kubeconfigPath)
				if err != nil {
					w.mu.Unlock()
					zlog.Error().Err(err).Caller().Send()
					break
				}
				w.kubeConfig = kubeConfig
				w.mu.Unlock()

				// Publish event
				w.eventbus.Publish("ADDED", kubeConfig)
			case fsEv.Op&fsnotify.Write == fsnotify.Write:
				// Load config
				w.mu.Lock()
				oldConfig := w.kubeConfig
				newConfig, err := clientcmd.LoadFromFile(w.kubeconfigPath)
				if err != nil {
					w.mu.Unlock()
					zlog.Error().Err(err).Caller().Send()
					break
				}
				w.kubeConfig = newConfig
				w.mu.Unlock()

				// Publish event
				w.eventbus.Publish("MODIFIED", oldConfig, newConfig)
			case fsEv.Op&fsnotify.Remove == fsnotify.Remove:
				w.mu.Lock()
				oldConfig := w.kubeConfig
				w.kubeConfig = nil
				w.mu.Unlock()

				// Publish event
				w.eventbus.Publish("DELETED", oldConfig)
			}
		}
	}
}
