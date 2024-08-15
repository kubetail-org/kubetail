// Copyright 2024 Andres Morey
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

package grpchelpers

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type ClientConnInterface interface {
	grpc.ClientConnInterface
	Close() error
}

type ConnectionManagerInterface interface {
	Start(ctx context.Context) error
	Get(nodeName string) ClientConnInterface
	GetAll() map[string]ClientConnInterface
	Teardown()
}

type ConnectionManager struct {
	mu        sync.Mutex
	conns     map[string]ClientConnInterface
	clientset kubernetes.Interface
	namespace string
	port      int
	cancel    context.CancelFunc
	isRunning bool
}

// Start
func (cm *ConnectionManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check if running
	if cm.isRunning {
		return nil
	}

	// init cancel func
	_, cancel := context.WithCancel(ctx)
	cm.cancel = cancel

	watchlist := cache.NewListWatchFromClient(
		cm.clientset.CoreV1().RESTClient(),
		"pods",
		cm.namespace,
		fields.Everything(),
	)

	// init informer with 5 minute resync period
	_, controller := cache.NewInformer(
		watchlist,
		&metav1.PartialObjectMetadata{},
		5*time.Minute,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				fmt.Println("Pod added:", obj)
			},
			DeleteFunc: func(obj interface{}) {
				fmt.Println("Pod deleted:", obj)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				fmt.Println("Pod updated:", newObj)
			},
		},
	)

	stop := make(chan struct{})
	defer close(stop)

	// Start watching
	fmt.Println("Starting pod watcher...")
	controller.Run(stop)

	// set flag
	cm.isRunning = true

	return nil
}

// Get gRPC connection for a specific node
func (cm *ConnectionManager) Get(nodeName string) ClientConnInterface {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.conns[nodeName]
}

// Get all gRPC connections (one per node)
func (cm *ConnectionManager) GetAll() map[string]ClientConnInterface {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	return cm.conns
}

// Teardown
func (cm *ConnectionManager) Teardown() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check flag
	if !cm.isRunning {
		return
	}

	// cancel context
	cm.cancel()

	// close grpc connections
	for _, conn := range cm.conns {
		conn.Close()
	}

	// reset map
	cm.conns = make(map[string]ClientConnInterface)

	// set flag
	cm.isRunning = false
}

// Add a gRPC connection
func (cm *ConnectionManager) add(nodeName string, conn ClientConnInterface) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.conns[nodeName] = conn
}

// Remove a gRPC connection
func (cm *ConnectionManager) remove(nodeName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// close if exists
	if conn, exists := cm.conns[nodeName]; exists {
		conn.Close()
	}

	// remove from map
	delete(cm.conns, nodeName)
}

// Create new ConnectionManager instance
func NewConnectionManager(port int) (*ConnectionManager, error) {
	// config k8s
	// TODO: should connection manager support out-of-cluster config?
	k8scfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(k8scfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// get current namespace from file system
	nsPathname := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	nsBytes, err := os.ReadFile(nsPathname)
	if err != nil {
		return nil, fmt.Errorf("unable to read current namespace from %s: %v", nsPathname, err)
	}

	// Convert the byte slice to a string and return
	return &ConnectionManager{
		conns:     make(map[string]ClientConnInterface),
		clientset: clientset,
		namespace: string(nsBytes),
		port:      port,
	}, nil
}

/*
type ConnectionManager struct {
	mu           sync.Mutex
	k8sClientset kubernetes.Interface
	cancel       context.CancelFunc
	conns        map[string]*grpc.ClientConn
}

// add
func (cm *ConnectionManager) add(nodeName string, conn *grpc.ClientConn) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.conns[nodeName] = conn
}

// delete
func (cm *ConnectionManager) delete(nodeName string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.conns, nodeName)
}

// Get
func (cm *ConnectionManager) Get(nodeName string) *grpc.ClientConn {
	return cm.conns[nodeName]
}

// GetAll
func (cm *ConnectionManager) GetAll() map[string]*grpc.ClientConn {
	return cm.conns
}

// Teardown
func (cm *ConnectionManager) Teardown() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// cancel context
	cm.cancel()

	// close grpc connections
	for _, conn := range cm.conns {
		conn.Close()
	}
}

// NewGrpcConnectionManager
func NewConnectionManager(cfg *config.Config, k8sCfg *rest.Config) (*ConnectionManager, error) {
	clientset, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cm := &ConnectionManager{
		k8sClientset: clientset,
		cancel:       cancel,
		conns:        make(map[string]*grpc.ClientConn),
	}

	// get agent port from config
	port := "50051"
	parts := strings.Split(cfg.Agent.Addr, ":")
	if len(parts) == 2 {
		port = parts[1]
	}

	go func() {
		ls := labels.SelectorFromSet(labels.Set{
			"app.kubernetes.io/name":      "kubetail",
			"app.kubernetes.io/component": "agent",
		}).String()

		options := metav1.ListOptions{LabelSelector: ls}
		watchAPI, err := clientset.CoreV1().Pods("default").Watch(ctx, options)
		if err != nil {
			panic(err)
		}
		defer watchAPI.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watchAPI.ResultChan():
				if !ok {
					zlog.Warn().Msg("Watch channel closed, restarting...")
					watchAPI, err = clientset.CoreV1().Pods("default").Watch(ctx, options)
					if err != nil {
						panic(err)
					}
					continue
				}

				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					zlog.Error().Msgf("unexpected type: %v", event.Object)
					continue
				}

				switch event.Type {
				case "ADDED", "MODIFIED":
					if isPodRunning(pod) {
						zlog.Debug().Msgf("connecting to %s", pod.Status.PodIP)
						addr := ptr.To(fmt.Sprintf("%s:"+port, pod.Status.PodIP))
						conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
						if err != nil {
							panic(err)
						}
						cm.add(pod.Spec.NodeName, conn)
					}
				case "DELETED":
					cm.delete(pod.Spec.NodeName)
					fmt.Printf("Pod deleted: %s\n", pod.Name)
				}
			}
		}

	}()

	return cm, nil
}

func isPodRunning(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return false
		}
	}
	return true
}
*/
