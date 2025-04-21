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
	"net/http"
	"sync"
	"time"

	zlog "github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/config"
)

// Represents shared informer factory manager cache key
type fmCacheKey struct {
	kubeContext string
	bearerToken string
}

// Signature for permission checker function
type CheckPermissionsFunc func(clientset *kubernetes.Clientset) error

// ConnectionManager interface
type ConnectionManager interface {
	GetOrCreateRestConfig(kubeContext *string) (*rest.Config, error)
	GetOrCreateClientset(kubeContext *string) (kubernetes.Interface, error)
	GetOrCreateDynamicClient(kubeContext *string) (dynamic.Interface, error)
	GetOrCreateSharedInformerFactory(kubeContext *string, bearerToken string, namespace string, checkPermissions CheckPermissionsFunc) (informers.SharedInformerFactory, <-chan struct{}, error)
	GetDefaultNamespace(kubeContext *string) string
	DerefKubeContext(kubeContext *string) string
	WaitUntilReady(ctx context.Context, kubeContext *string) error
	Shutdown(ctx context.Context) error
}

// Represents DesktopConnectionManager
type DesktopConnectionManager struct {
	KubeConfigWatcher *KubeConfigWatcher
	kubeConfig        *api.Config
	kubeconfigPath    string
	rcCache           map[string]*rest.Config
	csCache           map[string]*kubernetes.Clientset
	dcCache           map[string]*dynamic.DynamicClient
	fmCache           map[fmCacheKey]SharedInformerFactoryManager
	rootCtx           context.Context
	rootCtxCancel     context.CancelFunc
	readyChs          map[string]chan struct{}
	mu                sync.Mutex
}

// Initialize new DesktopConnectionManager instance
func NewDesktopConnectionManager(options ...ConnectionManagerOption) (*DesktopConnectionManager, error) {
	cm := &DesktopConnectionManager{
		rcCache:  make(map[string]*rest.Config),
		csCache:  make(map[string]*kubernetes.Clientset),
		dcCache:  make(map[string]*dynamic.DynamicClient),
		fmCache:  make(map[fmCacheKey]SharedInformerFactoryManager),
		readyChs: make(map[string]chan struct{}),
		rootCtx:  context.Background(),
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

	// Warm up cache in background
	go cm.warmUpCache()

	// Register kube config watch handlers
	kfw.Subscribe("ADDED", cm.kubeConfigAdded)
	kfw.Subscribe("MODIFIED", cm.kubeConfigModified)
	kfw.Subscribe("DELETED", cm.kubeConfigDeleted)

	return cm, nil
}

// Stop bacgkround listeners and close underlying connections
func (cm *DesktopConnectionManager) Shutdown(ctx context.Context) error {
	cm.rootCtxCancel()

	// Initialize shutdown of shared informer factory managers
	var wg sync.WaitGroup
	for _, fm := range cm.fmCache {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fm.Shutdown(ctx)
		}()
	}

	// Unsubscribe from config watcher events and close
	cm.KubeConfigWatcher.Unsubscribe("ADDED", cm.kubeConfigAdded)
	cm.KubeConfigWatcher.Unsubscribe("MODIFIED", cm.kubeConfigModified)
	cm.KubeConfigWatcher.Unsubscribe("DELETED", cm.kubeConfigDeleted)
	cm.KubeConfigWatcher.Close()

	// Wait for shutdown to complete
	wg.Wait()

	return ctx.Err()
}

// Get cached REST config or create a new one
func (cm *DesktopConnectionManager) GetOrCreateRestConfig(kubeContextPtr *string) (*rest.Config, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
	return cm.getOrCreateRestConfig_UNSAFE(kubeContext)
}

// Get cached Clientset or create a new one
func (cm *DesktopConnectionManager) GetOrCreateClientset(kubeContextPtr *string) (kubernetes.Interface, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
	return cm.getOrCreateClientset_UNSAFE(kubeContext)
}

// Get cached dynamic client or create a new one
func (cm *DesktopConnectionManager) GetOrCreateDynamicClient(kubeContextPtr *string) (dynamic.Interface, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
	return cm.getOrCreateDynamicClient_UNSAFE(kubeContext)
}

// Get shared informer factory or create a new one
func (cm *DesktopConnectionManager) GetOrCreateSharedInformerFactory(kubeContextPtr *string, bearerToken string, namespace string, checkPermissions CheckPermissionsFunc) (informers.SharedInformerFactory, <-chan struct{}, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Init cache key
	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
	k := fmCacheKey{kubeContext, bearerToken}

	// Check cache
	fm, exists := cm.fmCache[k]
	if exists {
		return fm.GetOrCreateFactory(namespace)
	}

	// Clone rest config and set bearer token
	clientConfig := clientcmd.NewNonInteractiveClientConfig(*cm.kubeConfig, kubeContext, &clientcmd.ConfigOverrides{}, nil)
	rc, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	if bearerToken != "" {
		rc.BearerTokenFile = ""
		rc.BearerToken = bearerToken
	}

	// Init clientset
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, nil, err
	}

	// Check permissions
	if err := checkPermissions(clientset); err != nil {
		return nil, nil, err
	}

	// Create new factory manager
	fm = NewSharedInformerFactoryManager(clientset)

	// Add to cache
	cm.fmCache[k] = fm

	return fm.GetOrCreateFactory(namespace)
}

// GetDefaultNamespace
func (cm *DesktopConnectionManager) GetDefaultNamespace(kubeContextPtr *string) string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)
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
func (cm *DesktopConnectionManager) WaitUntilReady(ctx context.Context, kubeContextPtr *string) error {
	cm.mu.Lock()
	kubeContext := ptr.Deref(kubeContextPtr, cm.kubeConfig.CurrentContext)

	// Check cache
	if readyCh, exists := cm.readyChs[kubeContext]; exists {
		cm.mu.Unlock()

		// Wait until channel is closed
		<-readyCh

		return nil
	}

	// Create channel and add it to the cache
	readyCh := make(chan struct{})
	cm.readyChs[kubeContext] = readyCh

	// Get clientset
	clientset, err := cm.getOrCreateClientset_UNSAFE(kubeContext)
	if err != nil {
		cm.mu.Unlock()
		return err
	}

	cm.mu.Unlock()

	// Make a lightweight API call to warm up http connections
	// NOTE: all clients that share rest config will get warmed up automatically
	clientset.Discovery().ServerVersion()

	// Send stop signal to channel
	close(readyCh)

	return nil
}

// Get kube config
func (cm *DesktopConnectionManager) GetKubeConfig() *api.Config {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.kubeConfig
}

// Get or create REST config (not thread safe)
func (cm *DesktopConnectionManager) getOrCreateRestConfig_UNSAFE(kubeContext string) (*rest.Config, error) {
	// Check cache
	if rc, exists := cm.rcCache[kubeContext]; exists {
		return rc, nil
	}

	// Create new REST config
	clientConfig := clientcmd.NewNonInteractiveClientConfig(*cm.kubeConfig, kubeContext, &clientcmd.ConfigOverrides{}, nil)
	rc, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	// Add authentication handler
	rc.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
		return NewBearerTokenRoundTripper(transport)
	}

	// Add to cache
	cm.rcCache[kubeContext] = rc

	return rc, nil
}

// Get or create clientset (not thread safe)
func (cm *DesktopConnectionManager) getOrCreateClientset_UNSAFE(kubeContext string) (*kubernetes.Clientset, error) {
	// Check cache
	if clientset, exists := cm.csCache[kubeContext]; exists {
		return clientset, nil
	}

	// Get rest config
	restConfig, err := cm.getOrCreateRestConfig_UNSAFE(kubeContext)
	if err != nil {
		return nil, err
	}

	// Create client
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	// Add to cache
	cm.csCache[kubeContext] = clientset

	return clientset, nil
}

// Get or create dynamic client (not thread safe)
func (cm *DesktopConnectionManager) getOrCreateDynamicClient_UNSAFE(kubeContext string) (*dynamic.DynamicClient, error) {
	// Check cache
	if dynamicClient, exists := cm.dcCache[kubeContext]; exists {
		return dynamicClient, nil
	}

	// Get rest config
	restConfig, err := cm.getOrCreateRestConfig_UNSAFE(kubeContext)
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
	cm.dcCache[kubeContext] = dynamicClient

	return dynamicClient, nil
}

// Warm up cache in background
func (cm *DesktopConnectionManager) warmUpCache() {
	cm.mu.Lock()
	kubeConfig := cm.kubeConfig
	cm.mu.Unlock()

	ctx, cancel := context.WithTimeout(cm.rootCtx, 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for contextName := range kubeConfig.Contexts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.WaitUntilReady(ctx, ptr.To(contextName))
		}()
	}

	wg.Wait()

	if ctx.Err() != nil {
		zlog.Error().Err(ctx.Err()).Caller().Send()
	}
}

// Handle kube config ADDED event
func (cm *DesktopConnectionManager) kubeConfigAdded(config *api.Config) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.kubeConfig = config
}

// Handle kube config MODIFIED event
func (cm *DesktopConnectionManager) kubeConfigModified(oldConfig *api.Config, newConfig *api.Config) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.kubeConfig = newConfig
}

// Handle kube config DELETED event
func (cm *DesktopConnectionManager) kubeConfigDeleted(oldConfig *api.Config) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.kubeConfig = &api.Config{}
}

// Represents InClusterConnectionManager
type InClusterConnectionManager struct {
	restConfig    *rest.Config
	clientset     *kubernetes.Clientset
	dynamicClient *dynamic.DynamicClient
	fmCache       map[string]SharedInformerFactoryManager
	mu            sync.Mutex
}

// Initialize new InClusterConnectionManager instance
func NewInClusterConnectionManager(options ...ConnectionManagerOption) (*InClusterConnectionManager, error) {
	cm := &InClusterConnectionManager{
		fmCache: make(map[string]SharedInformerFactoryManager),
	}

	// Apply options
	for _, option := range options {
		option(cm)
	}

	return cm, nil
}

// Stop bacgkround listeners and close underlying connections
func (cm *InClusterConnectionManager) Shutdown(ctx context.Context) error {
	// Initialize shutdown of shared informer factory managers
	var wg sync.WaitGroup
	for _, fm := range cm.fmCache {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fm.Shutdown(ctx)
		}()
	}
	// Wait for shutdown to complete
	wg.Wait()

	return ctx.Err()
}

// Get cached Clientset or create a new one
func (cm *InClusterConnectionManager) GetOrCreateRestConfig(kubeContext *string) (*rest.Config, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.getOrCreateRestConfig_UNSAFE()
}

// Get cached Clientset or create a new one
func (cm *InClusterConnectionManager) GetOrCreateClientset(kubeContext *string) (kubernetes.Interface, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check cache
	if cm.clientset != nil {
		return cm.clientset, nil
	}

	// Get rest config
	restConfig, err := cm.getOrCreateRestConfig_UNSAFE()
	if err != nil {
		return nil, err
	}

	// Create client
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	// Add to cache
	cm.clientset = clientset

	return clientset, nil
}

// Get cached dynamic client or create a new one
func (cm *InClusterConnectionManager) GetOrCreateDynamicClient(kubeContext *string) (dynamic.Interface, error) {
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

// Get shared informer factory or create a new one
func (cm *InClusterConnectionManager) GetOrCreateSharedInformerFactory(kubeContextPtr *string, bearerToken string, namespace string, checkPermissions CheckPermissionsFunc) (informers.SharedInformerFactory, <-chan struct{}, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check cache
	fm, exists := cm.fmCache[bearerToken]
	if exists {
		return fm.GetOrCreateFactory(namespace)
	}

	// New rest config
	rc, err := rest.InClusterConfig()
	if err != nil {
		return nil, nil, err
	}

	if bearerToken != "" {
		rc.BearerTokenFile = ""
		rc.BearerToken = bearerToken
	}

	// Init clientset
	// TODO: use kubernetes.NewForConfigAndClient to re-use underlying transport
	clientset, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, nil, err
	}

	// Check permissions
	if err := checkPermissions(clientset); err != nil {
		return nil, nil, err
	}

	// Create new factory manager
	fm = NewSharedInformerFactoryManager(clientset)

	// Add to cache
	cm.fmCache[bearerToken] = fm

	return fm.GetOrCreateFactory(namespace)
}

// Get default namespace from local filesystem on pod
func (cm *InClusterConnectionManager) GetDefaultNamespace(kubeContext *string) string {
	return metav1.NamespaceDefault
}

// DerefKubeContext
func (cm *InClusterConnectionManager) DerefKubeContext(kubeContext *string) string {
	return ""
}

// Returns immediately in-cluster
func (cm *InClusterConnectionManager) WaitUntilReady(ctx context.Context, kubeContext *string) error {
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

	// Add authentication middleware
	rc.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
		return NewBearerTokenRoundTripper(transport)
	}

	// Add to cache
	cm.restConfig = rc

	return rc, nil
}

type ConnectionManagerOption func(cm ConnectionManager)

func WithKubeconfig(kubeconfig string) ConnectionManagerOption {
	return func(cm ConnectionManager) {
		switch t := cm.(type) {
		case *DesktopConnectionManager:
			t.kubeconfigPath = kubeconfig
		case *InClusterConnectionManager:
			break
		}
	}
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
