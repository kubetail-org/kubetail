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
	"context"
	"sync"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// SharedInformerFactoryManager interface
type SharedInformerFactoryManager interface {
	GetOrCreateFactory(namespace string) (informers.SharedInformerFactory, error)
	Shutdown(ctx context.Context) error
}

// Represents SharedInformerFactoryManager
type sharedInformerFactoryManager struct {
	clientset    kubernetes.Interface
	factoryCache map[string]informers.SharedInformerFactory
	mu           sync.Mutex
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// Initialize shared informer factory manager
func NewSharedInformerFactoryManager(clientset kubernetes.Interface) SharedInformerFactoryManager {
	return &sharedInformerFactoryManager{
		clientset:    clientset,
		factoryCache: make(map[string]informers.SharedInformerFactory),
		shutdownCh:   make(chan struct{}),
	}
}

// GetOrCreateFactory implementation
func (m *sharedInformerFactoryManager) GetOrCreateFactory(namespace string) (informers.SharedInformerFactory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cache
	if factory, exists := m.factoryCache[namespace]; exists {
		return factory, nil
	}

	// Create factory
	factory := informers.NewSharedInformerFactoryWithOptions(m.clientset, 0, informers.WithNamespace(namespace))

	// Start
	factory.Start(m.shutdownCh)

	// Add to cache
	m.factoryCache[namespace] = factory

	return factory, nil
}

// Shutdown all factories
func (m *sharedInformerFactoryManager) Shutdown(ctx context.Context) error {
	// Issue shutdown signal
	m.shutdownOnce.Do(func() {
		close(m.shutdownCh)
	})

	// Wait for factories to shutdown
	var wg sync.WaitGroup
	for _, factory := range m.factoryCache {
		wg.Add(1)
		go func(factory informers.SharedInformerFactory) {
			defer wg.Done()
			factory.Shutdown()
		}(factory)
	}

	// Wait for shutdown to complete or context to close
	stopCh := make(chan struct{})

	go func() {
		wg.Wait()
		close(stopCh)
	}()

	select {
	case <-ctx.Done():
		// Aborted
		return ctx.Err()
	case <-stopCh:
		// Finished gracefully
		return nil
	}
}
