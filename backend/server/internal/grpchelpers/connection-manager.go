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
	"strings"
	"sync"

	zlog "github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/backend/common/config"
)

type ConnectionManagerInterface interface {
	Get(nodeName string) *grpc.ClientConn
	GetAll() map[string]*grpc.ClientConn
}

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
			fmt.Println("xx")
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
					fmt.Printf("Unexpected type: %v\n", event.Object)
					continue
				}

				switch event.Type {
				case "ADDED", "MODIFIED":
					if isPodRunning(pod) {
						fmt.Printf("connecting to %s\n", pod.Status.PodIP)
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
