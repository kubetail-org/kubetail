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

package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	zlog "github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/common/k8shelpers"
)

// Represents local tunnel to remote service
type Tunnel struct {
	portForwarder *portforward.PortForwarder
	stopCh        chan struct{}
	wg            *sync.WaitGroup
}

// Start tunnel
func (t *Tunnel) Start() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := t.portForwarder.ForwardPorts(); err != nil {
			zlog.Fatal().Err(err).Send()
		}
	}()
	t.wg = &wg
}

// Graceful shutdown controlled by context
func (t *Tunnel) Shutdown(ctx context.Context) error {
	close(t.stopCh)
	t.wg.Wait()
	return nil
}

func NewTunnel(namespace, serviceName string, remotePort, localPort int) (*Tunnel, error) {
	cfg := config.DefaultConfig()
	cfg.AuthMode = config.AuthModeLocal

	k8sCfg, err := k8shelpers.Configure(cfg)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		return nil, err
	}

	// Find service
	service, err := clientset.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Find pods for the service using recommended label selector method
	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: service.Spec.Selector,
	})
	if err != nil {
		return nil, err
	}

	podList, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing pods for service %s: %v", serviceName, err)
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for service %s", serviceName)
	}

	podName := podList.Items[0].Name

	// Create a local port forward to the service
	url := clientset.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("portforward").
		URL()

	roundTripper, upgrader, _ := spdy.RoundTripperFor(k8sCfg)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, "POST", url)

	ports := []string{fmt.Sprintf("%d:%d", localPort, remotePort)}
	stopChan := make(chan struct{})
	readyChan := make(chan struct{})
	pf, err := portforward.New(dialer, ports, stopChan, readyChan, os.Stdout, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("error creating port forwarder: %v", err)
	}

	return &Tunnel{portForwarder: pf, stopCh: stopChan}, nil
}