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

	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
)

type ConnectionManager struct {
	mu        sync.Mutex
	conns     map[string]ClientConnInterface
	clientset kubernetes.Interface
	namespace string
	port      int
	stopCh    chan struct{}
	isRunning bool
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

// Start background process to monitor kubetail-agent pods and
// initialize grpc connections to them
func (cm *ConnectionManager) Start(ctx context.Context) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check if running
	if cm.isRunning {
		return
	}

	// init listwatch
	labelSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/name":      "kubetail",
		"app.kubernetes.io/component": "agent",
	}).String()

	watchlist := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector
			return cm.clientset.CoreV1().Pods(cm.namespace).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector
			return cm.clientset.CoreV1().Pods(cm.namespace).Watch(context.Background(), options)
		},
	}

	// init informer with 5 minute resync period
	_, controller := cache.NewInformer(
		watchlist,
		&corev1.Pod{},
		5*time.Minute,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				zlog.Debug().Msgf("[grpc-connection-manager] pod added: %v", obj)
				pod := obj.(*corev1.Pod)
				if isPodRunning(pod) {
					zlog.Debug().Msgf("connecting to %s", pod.Status.PodIP)
					addr := ptr.To(fmt.Sprintf("%s:%d", pod.Status.PodIP, cm.port))
					conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
					if err != nil {
						panic(err)
					}
					cm.add(pod.Spec.NodeName, conn)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				zlog.Debug().Msgf("[grpc-connection-manager] pod updated: %v", newObj)
				pod := newObj.(*corev1.Pod)
				if isPodRunning(pod) {
					zlog.Debug().Msgf("connecting to %s", pod.Status.PodIP)
					addr := ptr.To(fmt.Sprintf("%s:%d", pod.Status.PodIP, cm.port))
					conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
					if err != nil {
						panic(err)
					}
					cm.add(pod.Spec.NodeName, conn)
				}
			},
			DeleteFunc: func(obj interface{}) {
				zlog.Debug().Msgf("[grpc-connection-manager] pod deleted: %v", obj)
				pod := obj.(*corev1.Pod)
				cm.remove(pod.Spec.NodeName)
			},
		},
	)

	cm.stopCh = make(chan struct{})

	// run watcher in a go routine
	go controller.Run(cm.stopCh)

	// set flag
	cm.isRunning = true

	// handle context stopping in a goroutine
	go func() {
		<-ctx.Done()

		cm.mu.Lock()
		defer cm.mu.Unlock()

		close(cm.stopCh)
		cm.isRunning = false
	}()
}

// Teardown
func (cm *ConnectionManager) Teardown() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// check flag
	if !cm.isRunning {
		return
	}

	// stop informer
	close(cm.stopCh)

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
