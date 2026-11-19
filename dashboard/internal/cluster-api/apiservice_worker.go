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

package clusterapi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kubetail-org/megaphone"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	aggregatorclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	aggregatorinformers "k8s.io/kube-aggregator/pkg/client/informers/externalversions"
)

// apiServiceHealthMonitorWorker watches the v1.api.kubetail.com APIService and
// derives health from its Available condition.
type apiServiceHealthMonitorWorker struct {
	lastStatus HealthStatus
	factory    aggregatorinformers.SharedInformerFactory
	informer   cache.SharedIndexInformer
	apiSvc     *apiregistrationv1.APIService
	mp         megaphone.Megaphone[HealthStatus]
	shutdownCh chan struct{}
	mu         sync.RWMutex
}

func newAPIServiceHealthMonitorWorker(client aggregatorclientset.Interface) (*apiServiceHealthMonitorWorker, error) {
	factory := aggregatorinformers.NewSharedInformerFactoryWithOptions(client, 10*time.Minute,
		aggregatorinformers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", APIServiceName).String()
		}),
	)

	informer := factory.Apiregistration().V1().APIServices().Informer()

	w := &apiServiceHealthMonitorWorker{
		lastStatus: HealthStatusUnknown,
		factory:    factory,
		informer:   informer,
		mp:         megaphone.New[HealthStatus](),
		shutdownCh: make(chan struct{}),
	}

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onInformerAdd,
		UpdateFunc: w.onInformerUpdate,
		DeleteFunc: w.onInformerDelete,
	}); err != nil {
		return nil, err
	}

	return w, nil
}

func (w *apiServiceHealthMonitorWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.factory.Start(w.shutdownCh)

	if !cache.WaitForCacheSync(ctx.Done(), w.informer.HasSynced) {
		return fmt.Errorf("failed to sync")
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Seed apiSvc directly from the store. WaitForCacheSync only guarantees the
	// store is populated, not that AddFunc has run — and AddFunc is blocked on
	// w.mu, which we hold. Reading the store here avoids that race.
	for _, obj := range w.informer.GetStore().List() {
		if apiSvc := asTarget(obj); apiSvc != nil {
			w.apiSvc = apiSvc
			break
		}
	}

	w.updateHealthStatus_UNSAFE()

	return nil
}

func (w *apiServiceHealthMonitorWorker) Shutdown() {
	close(w.shutdownCh)
	w.factory.Shutdown()
}

func (w *apiServiceHealthMonitorWorker) GetHealthStatus() HealthStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastStatus
}

func (w *apiServiceHealthMonitorWorker) WatchHealthStatus(ctx context.Context) (<-chan HealthStatus, error) {
	outCh := make(chan HealthStatus)

	var mu sync.Mutex
	var lastStatus *HealthStatus

	sendStatus := func(newStatus HealthStatus) {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() == nil && (lastStatus == nil || *lastStatus != newStatus) {
			lastStatus = &newStatus
			outCh <- newStatus
		}
	}

	sub, err := w.mp.Subscribe("UPDATE", sendStatus)
	if err != nil {
		return nil, err
	}

	go func() {
		sendStatus(w.GetHealthStatus())
		<-ctx.Done()
		sub.Drain()
		close(outCh)
	}()

	return outCh, nil
}

func (w *apiServiceHealthMonitorWorker) ReadyWait(ctx context.Context) error {
	if w.GetHealthStatus() == HealthStatusSuccess {
		return nil
	}

	ch, err := w.WatchHealthStatus(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status := <-ch:
			if status == HealthStatusSuccess {
				return nil
			}
		}
	}
}

// asTarget returns the object as an APIService if it is the one we monitor,
// or nil otherwise. The factory's field selector filters server-side in
// production, but fake clientsets don't honor it — so we re-check by name.
func asTarget(obj any) *apiregistrationv1.APIService {
	apiSvc, ok := obj.(*apiregistrationv1.APIService)
	if !ok || apiSvc.Name != APIServiceName {
		return nil
	}
	return apiSvc
}

func (w *apiServiceHealthMonitorWorker) onInformerAdd(obj any) {
	apiSvc := asTarget(obj)
	if apiSvc == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apiSvc = apiSvc
	w.updateHealthStatus_UNSAFE()
}

func (w *apiServiceHealthMonitorWorker) onInformerUpdate(_, newObj any) {
	apiSvc := asTarget(newObj)
	if apiSvc == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apiSvc = apiSvc
	w.updateHealthStatus_UNSAFE()
}

func (w *apiServiceHealthMonitorWorker) onInformerDelete(obj any) {
	// Unwrap tombstone delivered when the informer missed the delete event;
	// otherwise asTarget would reject it and leave w.apiSvc reporting stale health.
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	if asTarget(obj) == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.apiSvc = nil
	w.updateHealthStatus_UNSAFE()
}

func (w *apiServiceHealthMonitorWorker) getHealthStatus_UNSAFE() HealthStatus {
	if w.apiSvc == nil {
		return HealthStatusNotFound
	}

	for _, cond := range w.apiSvc.Status.Conditions {
		if cond.Type != apiregistrationv1.Available {
			continue
		}
		switch cond.Status {
		case apiregistrationv1.ConditionTrue:
			return HealthStatusSuccess
		case apiregistrationv1.ConditionFalse:
			return HealthStatusFailure
		}
	}

	// APIService exists but no Available condition has been reported yet.
	return HealthStatusPending
}

func (w *apiServiceHealthMonitorWorker) updateHealthStatus_UNSAFE() {
	newStatus := w.getHealthStatus_UNSAFE()

	// Fold transient Failure on cold start / flap into Pending.
	if newStatus == HealthStatusFailure && (w.lastStatus == HealthStatusNotFound || w.lastStatus == HealthStatusPending) {
		newStatus = HealthStatusPending
	}

	if newStatus != w.lastStatus {
		w.lastStatus = newStatus
		w.mp.Publish("UPDATE", newStatus)
	}
}
