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

package fannypack

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// Represents interest in pod ips that are part of a Kubernetes service
type Subscription struct{}

// Ends subscription
func (sub *Subscription) Unsubscribe() error {
	panic("not implemented")
}

// A Dispatcher is a utility that facilitates sending queries to multiple grpc servers
// simultaneously. It maintains an up-to-date list of all pod ips that are part of a
// Kubernetes service and directs queries to the ips.
type Dispatcher struct {
	serviceName string
	namespace   string
	clientset   *kubernetes.Clientset
	informer    cache.SharedInformer
	stopCh      chan struct{}
}

// Sends queries to all available ips at query-time
func (d *Dispatcher) Fanout(ctx context.Context, fn DispatchHandler) error {
	panic("not implementeted")
}

// Sends queries to all available ips at query-time and all subsequent ips when
// they become available until Unsubscribe() is called
func (d *Dispatcher) FanoutSubscribe(ctx context.Context, fn DispatchHandler) (*Subscription, error) {
	panic("not implementeted")
}

// Stops all background processes and closes the underlying grpc client connection
func (d *Dispatcher) Stop() {
	// stop informer
	close(d.stopCh)
}

// Starts background processes
func (d *Dispatcher) start() error {
	informer := cache.NewSharedInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return d.clientset.DiscoveryV1().EndpointSlices(d.namespace).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return d.clientset.DiscoveryV1().EndpointSlices(d.namespace).Watch(context.Background(), options)
			},
		},
		&discoveryv1.EndpointSlice{},
		10*time.Minute,
	)
	d.informer = informer

	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// todo
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			// todo
		},
		DeleteFunc: func(obj interface{}) {
			// todo
		},
	})
	if err != nil {
		return err
	}

	d.stopCh = make(chan struct{})

	// run informer in go routines
	go informer.Run(d.stopCh)

	return nil
}

func Connect(connectUrl string) (*Dispatcher, error) {
	// config k8s
	k8scfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(k8scfg)
	if err != nil {
		return nil, err
	}

	// parse url
	u, err := url.Parse(connectUrl)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(u.Host, ".")

	serviceName := parts[0]

	// get namespace
	var namespace string
	if len(parts) > 1 {
		namespace = parts[1]
	} else {
		nsPathname := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		nsBytes, err := os.ReadFile(nsPathname)
		if err != nil {
			return nil, fmt.Errorf("unable to read current namespace from %s: %v", nsPathname, err)
		}
		namespace = string(nsBytes)
	}

	// init dispatcher and start background processes
	d := &Dispatcher{
		serviceName: serviceName,
		namespace:   namespace,
		clientset:   clientset,
	}
	d.start()

	return d, nil
}
