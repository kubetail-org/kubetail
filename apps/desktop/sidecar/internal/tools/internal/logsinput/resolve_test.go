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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/logsinput/logsinputtest"
)

// Every refusal of the service reaches the model with the code it carries, and an
// unmirrored source with its index; any other error passes through.
func TestARefusalKeepsItsCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want *Refused
	}{
		{logs.ErrTooMany, &Refused{Code: refusal.CodeTooMany, Source: -1}},
		{refusal.Refusal{Code: refusal.CodeTooMany, Reason: "the cache answered more than Kubetail reads"}, &Refused{Code: refusal.CodeTooMany, Source: -1}},
		{refusal.Refusal{Code: refusal.CodeBadScope, Reason: "bad kind"}, &Refused{Code: refusal.CodeBadScope, Source: -1}},
		{refusal.Unmirrored{Refusal: refusal.Refusal{Code: refusal.CodeNotMirrored, Reason: "no jobs"}, Source: 2}, &Refused{Code: refusal.CodeNotMirrored, Source: 2}},
		{refusal.NoNodes{Refusal: refusal.Refusal{Code: refusal.CodeNoNodes, Reason: "no nodes"}}, &Refused{Code: refusal.CodeNoNodes, Source: -1}},
	} {
		assert.Equal(t, tc.want, refusedBy(tc.err), tc.err.Error())
	}
	err := errors.New("db closed")
	assert.Same(t, err, refusedBy(err))
	assert.Equal(t, "logs: refused: too-many", (&Refused{Code: refusal.CodeTooMany, Source: -1}).Error())
}

// A view whose mirror cannot be read fails with the failure: every statement, or the
// count of a workload's pods for its previous instances.
func TestAViewTheMirrorCannotReadFails(t *testing.T) {
	t.Parallel()
	failed := errors.New("disk I/O error")
	previous := Input{Sources: []Source{{Namespace: "prod", Kind: tools.LogsSourceDeployment, Name: "webapp", Previous: true}}}
	every := logsinputtest.Over(nil)
	every.QueryErr = failed
	count := logsinputtest.Watching(t)
	count.QueryErr, count.FailIn = failed, "count(*)"
	for name, f := range map[string]*logsinputtest.Service{"every statement": every, "the pods' count": count} {
		_, _, err := Resolve(t.Context(), f, time.Now(), "7", previous)
		assert.ErrorIs(t, err, failed, name)
	}
}
