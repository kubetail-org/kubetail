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

package logsinput

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// The reading's bounds: a source's containers, and the statements logs.Check
// runs, each of which the service limits itself.
const (
	maxRows  = 10_000
	maxBytes = tools.FileLimit
)

// Refused is a view the mirror refuses: the code the model reads, the source it
// concerns (-1 for the whole view), and for a container it does not have, the
// ones it does.
type Refused struct {
	Code       refusal.Code
	Source     int
	Containers []string
}

func (e *Refused) Error() string { return "logs: refused: " + string(e.Code) }

// Note is what a tool says of one source beyond its name: what the mirror holds of
// it now, the containers it has, the default container the mirror shows now when
// it was picked among several, and for a workload with previous, how many of its
// pods have an instance to show.
type Note struct {
	state      logs.SourceState
	containers []string
	defaulted  string
	pods       int
	restarted  int
}

// Resolve is the view in reads, over one reading of the cluster's active cache:
// each source checked against the mirror, its containers defaulted and its
// previous instance found, resolved at now. Its anchor is the tool's to set.
func Resolve(ctx context.Context, svc cluster.Service, now time.Time, clusterID cluster.ClusterID, in Input) (tools.LogsViewAction, []Note, error) {
	var view tools.LogsViewAction
	var notes []Note
	err := svc.CachedData().ReadActive(ctx, clusterID, maxRows, maxBytes, func(q cluster.CacheReading, _ cluster.ActiveCluster) error {
		var err error
		view, notes, err = resolveIn(q, in)
		return err
	})
	if err != nil {
		return tools.LogsViewAction{}, nil, err
	}
	view.ResolvedAt = now.UTC()
	return view, notes, nil
}

// resolveIn is one reading's view, each source checked in turn, then the whole
// as the logs service resolves it, so a view the tool opens is one it reads. It
// builds the view afresh, since ReadActive may run it twice.
func resolveIn(q cluster.CacheReading, in Input) (tools.LogsViewAction, []Note, error) {
	view := tools.LogsViewAction{LogsScopeInput: tools.LogsScopeInput{Sources: []tools.LogsViewSource{}, Filters: in.Filters, Grep: in.Grep}}
	if view.Filters == nil {
		view.Filters = []tools.LogsViewFilter{}
	}
	var notes []Note
	for i, s := range in.Sources {
		src, n, err := resolveSource(q, i, s)
		if err != nil {
			return tools.LogsViewAction{}, nil, err
		}
		view.Sources = append(view.Sources, src)
		notes = append(notes, n)
	}
	states, err := logs.Check(q, view.LogsScopeInput)
	if err != nil {
		return tools.LogsViewAction{}, nil, refusedBy(err)
	}
	for i, src := range view.Sources {
		notes[i].state = states[i]
		if src.Previous && src.Kind != tools.LogsSourcePod {
			count, err := logs.CountPods(q, src)
			if err != nil {
				return tools.LogsViewAction{}, nil, err
			}
			notes[i].pods, notes[i].restarted = count.Pods, count.Previous
		}
	}
	return view, notes, nil
}

// refusedBy is the logs service's refusal of a view as the model reads it, with
// the refusal's own code.
func refusedBy(err error) error {
	r, ok := refusal.As(err)
	if !ok {
		return err
	}
	out := &Refused{Code: r.Code, Source: -1}
	if unmirrored, ok := errors.AsType[refusal.Unmirrored](err); ok {
		out.Source = unmirrored.Source
	}
	return out
}

// resolveSource is one source checked against the mirror: each container named
// is one of its object's. With none named the backend reads each pod's default
// container, and the receipt names the one the object has now. An object the
// mirror does not hold yet checks nothing.
func resolveSource(q cluster.CacheReading, i int, s Source) (tools.LogsViewSource, Note, error) {
	f, err := object(q, s)
	if err != nil {
		return tools.LogsViewSource{}, Note{}, err
	}
	src := tools.LogsViewSource{Namespace: s.Namespace, Kind: s.Kind, Name: s.Name, Containers: []string{}, AllContainers: s.AllContainers, Previous: s.Previous}
	n := Note{containers: f.names}
	switch {
	case s.AllContainers:
	case len(s.Containers) > 0:
		for _, c := range s.Containers {
			if f.uid != "" && !slices.Contains(f.names, c) {
				return tools.LogsViewSource{}, Note{}, &Refused{Code: refusal.CodeNoContainer, Source: i, Containers: f.names}
			}
		}
		src.Containers = s.Containers
	case len(f.names) > 1:
		n.defaulted = defaultContainer(f)
	}
	return src, n, nil
}

// defaultContainer is the one kubectl logs reads with none named: the one the
// annotation names when the object has it, else the first of its containers.
func defaultContainer(f found) string {
	if f.annotated != "" && slices.Contains(f.names, f.annotated) {
		return f.annotated
	}
	return f.names[0]
}
