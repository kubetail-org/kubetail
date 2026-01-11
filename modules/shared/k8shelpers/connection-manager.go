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
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/util"
)

// Represents shared informer factory cache key
type factoryCacheKey struct {
	kubeContext string
	namespace   string
}

// Signature for permission checker function
type CheckPermissionsFunc func(clientset *kubernetes.Clientset) error

// ConnectionManager interface
type ConnectionManager interface {
	GetOrCreateRestConfig(kubeContext string) (*rest.Config, error)
	GetOrCreateClientset(kubeContext string) (kubernetes.Interface, error)
	GetOrCreateDynamicClient(kubeContext string) (dynamic.Interface, error)
	GetDefaultNamespace(kubeContext string) string
	DerefKubeContext(kubeContext *string) string
	NewInformer(ctx context.Context, kubeContext string, token string, namespace string, gvr schema.GroupVersionResource) (informers.GenericInformer, func(), error)
	WaitUntilReady(ctx context.Context, kubeContext string) error
	Shutdown(ctx context.Context) error
}

// Represents DesktopConnectionManager
type DesktopConnectionManager struct {
	KubeConfigWatcher *KubeConfigWatcher
	kubeconfigPath    string
	kubeConfig        *api.Config
	isLazy            bool
	authorizer        DesktopAuthorizer
	rcCache           util.SyncGroup[string, *rest.Config]
	csCache           util.SyncGroup[string, *kubernetes.Clientset]
	dcCache           util.SyncGroup[string, *dynamic.DynamicClient]
	factoryCache      util.SyncGroup[factoryCacheKey, informers.SharedInformerFactory]
	isReadyCache      util.SyncGroup[string, bool]
	rootCtx           context.Context
	rootCtxCancel     context.CancelFunc
	stopCh            chan struct{}
	mu                sync.Mutex
}

// Initialize new DesktopConnectionManager instance
func NewDesktopConnectionManager(options ...ConnectionManagerOption) (*DesktopConnectionManager, error) {
	cm := &DesktopConnectionManager{
		authorizer: NewDesktopAuthorizer(),
		stopCh:     make(chan struct{}),
	}

	// Init root context
	cm.rootCtx, cm.rootCtxCancel = context.WithCancel(context.Background())

	// Apply options
	for _, option := range options {
		option(cm)
	}

	// Init KubeConfigWatcher
	kfw, err := NewKubeConfigWatcher(cm.kubeconfigPath)
	if err != nil {
		return nil, err
	}
	cm.KubeConfigWatcher = kfw

	// Cache kube config
	cm.kubeConfig = kfw.Get()

	// Warm up cache in background (if not lazy)
	if !cm.isLazy {
		go cm.warmUpCache()
	}

	// Register kube config watch handler
	kfw.Subscribe(cm.kubeConfigModified)

	return cm, nil
}

// Stop bacgkround listeners and close underlying connections
func (cm *DesktopConnectionManager) Shutdown(ctx context.Context) error {
	cm.rootCtxCancel()
	close(cm.stopCh)

	// Initialize shutdown of shared informer factory managers
	var wg sync.WaitGroup
	cm.factoryCache.Range(func(key factoryCacheKey, factory informers.SharedInformerFactory) bool {
		wg.Add(1)
		go func(f informers.SharedInformerFactory) {
			defer wg.Done()
			f.Shutdown()
		}(factory)
		return true
	})

	// Unsubscribe from config watcher events and close
	cm.KubeConfigWatcher.Unsubscribe(cm.kubeConfigModified)
	cm.KubeConfigWatcher.Close()

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

// Get cached REST config or create a new one
func (cm *DesktopConnectionManager) GetOrCreateRestConfig(kubeContext string) (*rest.Config, error) {
	v, _, err := cm.rcCache.LoadOrCompute(kubeContext, func() (*rest.Config, error) {
		kubeConfig := cm.GetKubeConfig()

		// Create new REST config
		clientConfig := clientcmd.NewNonInteractiveClientConfig(*kubeConfig, kubeContext, &clientcmd.ConfigOverrides{}, nil)
		rc, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, err
		}

		// Add authentication handler
		rc.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
			return NewBearerTokenRoundTripper(transport)
		}

		return rc, nil
	})
	return v, err
}

// Get cached Clientset or create a new one
func (cm *DesktopConnectionManager) GetOrCreateClientset(kubeContext string) (kubernetes.Interface, error) {
	return cm.getOrCreateClientset(kubeContext)
}

// Get cached dynamic client or create a new one
func (cm *DesktopConnectionManager) GetOrCreateDynamicClient(kubeContext string) (dynamic.Interface, error) {
	return cm.getOrCreateDynamicClient(kubeContext)
}

func (cm *DesktopConnectionManager) NewInformer(ctx context.Context, kubeContext string, token string, namespace string, gvr schema.GroupVersionResource) (informers.GenericInformer, func(), error) {
	// Get clientset
	clientset, err := cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		return nil, nil, err
	}

	// Check permission
	if err := cm.authorizer.IsAllowedInformer(ctx, clientset, namespace, gvr); err != nil {
		return nil, nil, err
	}

	// Get or create factory
	factory, err := cm.getOrCreateSharedInformerFactory(kubeContext, namespace)
	if err != nil {
		return nil, nil, err
	}

	// Init informer
	informer, err := factory.ForResource(gvr)
	if err != nil {
		return nil, nil, err
	}

	// Create start function
	startFn := func() {
		factory.Start(cm.stopCh)
	}

	return informer, startFn, nil
}

// GetDefaultNamespace
func (cm *DesktopConnectionManager) GetDefaultNamespace(kubeContext string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if kubeContext == "" {
		kubeContext = cm.kubeConfig.CurrentContext
	}

	context, exists := cm.kubeConfig.Contexts[kubeContext]
	if !exists || context.Namespace == "" {
		return metav1.NamespaceDefault
	}

	return context.Namespace
}

// DerefKubeContext
func (cm *DesktopConnectionManager) DerefKubeContext(kubeContextPtr *string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
}

// Sleep until clients have been initialized
func (cm *DesktopConnectionManager) WaitUntilReady(ctx context.Context, kubeContext string) error {
	_, _, err := cm.isReadyCache.LoadOrComputeWithContext(ctx, kubeContext, func() (bool, error) {
		// Get clientset
		clientset, err := cm.getOrCreateClientset(kubeContext)
		if err != nil {
			return false, err
		}

		// Make a lightweight API call to warm up http connections
		// NOTE: all clients that share rest config will get warmed up automatically
		_, err = clientset.Discovery().ServerVersion()
		if err != nil {
			return false, err
		}

		return true, nil
	})

	return err
}

// Get kube config
func (cm *DesktopConnectionManager) GetKubeConfig() *api.Config {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.kubeConfig
}

// Get or create clientset (thread safe)
func (cm *DesktopConnectionManager) getOrCreateClientset(kubeContext string) (*kubernetes.Clientset, error) {
	v, _, err := cm.csCache.LoadOrCompute(kubeContext, func() (*kubernetes.Clientset, error) {
		// Get rest config
		restConfig, err := cm.GetOrCreateRestConfig(kubeContext)
		if err != nil {
			return nil, err
		}

		// Create client
		// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}

		return clientset, nil
	})
	return v, err
}

// Get or create dynamic client (thread safe)
func (cm *DesktopConnectionManager) getOrCreateDynamicClient(kubeContext string) (*dynamic.DynamicClient, error) {
	v, _, err := cm.dcCache.LoadOrCompute(kubeContext, func() (*dynamic.DynamicClient, error) {
		// Get rest config
		restConfig, err := cm.GetOrCreateRestConfig(kubeContext)
		if err != nil {
			return nil, err
		}

		// Create client
		// TODO: use dynamic.NewForConfigAndClient to re-use underlying transport
		dynamicClient, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}

		return dynamicClient, nil
	})
	return v, err
}

// Get or create shared informer factory (thread safe)
func (cm *DesktopConnectionManager) getOrCreateSharedInformerFactory(kubeContext string, namespace string) (informers.SharedInformerFactory, error) {
	k := factoryCacheKey{kubeContext, namespace}

	v, _, err := cm.factoryCache.LoadOrCompute(k, func() (informers.SharedInformerFactory, error) {
		// Get or create clientset
		clientset, err := cm.getOrCreateClientset(kubeContext)
		if err != nil {
			return nil, err
		}

		// Create factory
		factory := informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithNamespace(namespace))

		// Start
		factory.Start(cm.stopCh)

		return factory, nil
	})

	return v, err
}

// Warm up cache in background
func (cm *DesktopConnectionManager) warmUpCache() {
	kubeConfig := cm.GetKubeConfig()

	ctx, cancel := context.WithTimeout(cm.rootCtx, 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for contextName := range kubeConfig.Contexts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.WaitUntilReady(ctx, contextName)
		}()
	}

	wg.Wait()
}

// Handle kube config modified event
func (cm *DesktopConnectionManager) kubeConfigModified(newConfig *api.Config) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.kubeConfig = newConfig
}

// Represents InClusterConnectionManager
type InClusterConnectionManager struct {
	restConfig    *rest.Config
	clientset     *kubernetes.Clientset
	dynamicClient *dynamic.DynamicClient
	authorizer    InClusterAuthorizer
	factoryCache  map[string]informers.SharedInformerFactory
	stopCh        chan struct{}
	mu            sync.Mutex
}

// Initialize new InClusterConnectionManager instance
func NewInClusterConnectionManager(options ...ConnectionManagerOption) (*InClusterConnectionManager, error) {
	cm := &InClusterConnectionManager{
		authorizer:   NewInClusterAuthorizer(),
		factoryCache: make(map[string]informers.SharedInformerFactory),
		stopCh:       make(chan struct{}),
	}

	// Apply options
	for _, option := range options {
		option(cm)
	}

	return cm, nil
}

// Stop bacgkround listeners and close underlying connections
func (cm *InClusterConnectionManager) Shutdown(ctx context.Context) error {
	close(cm.stopCh)

	// Initialize shutdown of shared informer factory managers
	var wg sync.WaitGroup
	for _, factory := range cm.factoryCache {
		wg.Add(1)
		go func() {
			defer wg.Done()
			factory.Shutdown()
		}()
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

// Get cached Clientset or create a new one
func (cm *InClusterConnectionManager) GetOrCreateRestConfig(kubeContext string) (*rest.Config, error) {
	if kubeContext != "" {
		return nil, fmt.Errorf("kubeContext is not supported")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.getOrCreateRestConfig_UNSAFE()
}

// Get cached Clientset or create a new one
func (cm *InClusterConnectionManager) GetOrCreateClientset(kubeContext string) (kubernetes.Interface, error) {
	if kubeContext != "" {
		return nil, fmt.Errorf("kubeContext is not supported")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.getOrCreateClientset_UNSAFE()
}

// Get cached dynamic client or create a new one
func (cm *InClusterConnectionManager) GetOrCreateDynamicClient(kubeContext string) (dynamic.Interface, error) {
	if kubeContext != "" {
		return nil, fmt.Errorf("kubeContext is not supported")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check cache
	if cm.dynamicClient != nil {
		return cm.dynamicClient, nil
	}

	// Get rest config
	restConfig, err := cm.getOrCreateRestConfig_UNSAFE()
	if err != nil {
		return nil, err
	}

	// Create client
	// TODO: use dynamic.NewForConfigAndClient to re-use underlying transport
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	// Add to cache
	cm.dynamicClient = dynamicClient

	return dynamicClient, nil
}

// Get generic informer or create new one
func (cm *InClusterConnectionManager) NewInformer(ctx context.Context, kubeContext string, token string, namespace string, gvr schema.GroupVersionResource) (informers.GenericInformer, func(), error) {
	if kubeContext != "" {
		return nil, nil, fmt.Errorf("kubeContext is not supported")
	}

	// Get rest config
	restConfig, err := cm.GetOrCreateRestConfig(kubeContext)
	if err != nil {
		return nil, nil, err
	}

	// Check permission
	if err := cm.authorizer.IsAllowedInformer(ctx, restConfig, token, namespace, gvr); err != nil {
		return nil, nil, err
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get or create factory
	factory, err := cm.getOrCreateSharedInformerFactory_UNSAFE(namespace)
	if err != nil {
		return nil, nil, err
	}

	// Init informer
	informer, err := factory.ForResource(gvr)
	if err != nil {
		return nil, nil, err
	}

	// Create start function
	startFn := func() {
		factory.Start(cm.stopCh)
	}

	return informer, startFn, nil
}

// Get default namespace from local filesystem on pod
func (cm *InClusterConnectionManager) GetDefaultNamespace(kubeContext string) string {
	return metav1.NamespaceDefault
}

// DerefKubeContext
func (cm *InClusterConnectionManager) DerefKubeContext(kubeContext *string) string {
	return ""
}

// Returns immediately in-cluster
func (cm *InClusterConnectionManager) WaitUntilReady(ctx context.Context, kubeContext string) error {
	return nil
}

// Get or create REST config
func (cm *InClusterConnectionManager) getOrCreateRestConfig_UNSAFE() (*rest.Config, error) {
	// Check cache
	if cm.restConfig != nil {
		return cm.restConfig, nil
	}

	// Create
	rc, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// Set rate limits
	rc.QPS = 10.0
	rc.Burst = 40

	// Add authentication middleware
	rc.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
		return NewBearerTokenRoundTripper(transport)
	}

	// Add to cache
	cm.restConfig = rc

	return rc, nil
}

// Get or create clientset (not thread safe)
func (cm *InClusterConnectionManager) getOrCreateClientset_UNSAFE() (kubernetes.Interface, error) {
	// Check cache
	if cm.clientset != nil {
		return cm.clientset, nil
	}

	// Get rest config
	rc, err := cm.getOrCreateRestConfig_UNSAFE()
	if err != nil {
		return nil, err
	}

	// Create client
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	// Add to cache
	cm.clientset = clientset

	return clientset, nil
}

// Get or create shared informer factory (not thread safe)
func (cm *InClusterConnectionManager) getOrCreateSharedInformerFactory_UNSAFE(namespace string) (informers.SharedInformerFactory, error) {
	// Check cache
	factory, exists := cm.factoryCache[namespace]
	if exists {
		return factory, nil
	}

	// Init clientset
	clientset, err := cm.getOrCreateClientset_UNSAFE()
	if err != nil {
		return nil, err
	}

	// Create factory
	factory = informers.NewSharedInformerFactoryWithOptions(clientset, 0, informers.WithNamespace(namespace))

	// Start
	factory.Start(cm.stopCh)

	// Add to cache
	cm.factoryCache[namespace] = factory

	return factory, nil
}

// Initialize new ConnectionManager depending on environment
func NewConnectionManager(env config.Environment, options ...ConnectionManagerOption) (ConnectionManager, error) {
	var cm ConnectionManager
	var err error
	switch env {
	case config.EnvironmentDesktop:
		cm, err = NewDesktopConnectionManager(options...)
	case config.EnvironmentCluster:
		cm, err = NewInClusterConnectionManager(options...)
	default:
		panic("not supported")
	}
	return cm, err
}

// Represents variadic option type for ConnectionManager
type ConnectionManagerOption func(cm ConnectionManager)

// WithKubeconfigPath sets kubeconfig file path
func WithKubeconfigPath(kubeconfigPath string) ConnectionManagerOption {
	return func(cm ConnectionManager) {
		switch t := cm.(type) {
		case *DesktopConnectionManager:
			t.kubeconfigPath = kubeconfigPath
		case *InClusterConnectionManager:
			break
		}
	}
}

// WithLazyConnect skips cache warmer
func WithLazyConnect(isLazy bool) ConnectionManagerOption {
	return func(cm ConnectionManager) {
		switch t := cm.(type) {
		case *DesktopConnectionManager:
			t.isLazy = isLazy
		case *InClusterConnectionManager:
			break
		}
	}
}
