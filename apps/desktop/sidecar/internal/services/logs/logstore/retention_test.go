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

package logstore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A stream's share is a quarter of the file.
func TestAStreamGetsAQuarterOfTheFile(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DefaultRetention.Bytes/4, DefaultRetention.StreamShare)
}

// A file holds a gibibyte, so a search across a view's logs stays cached, and a sweep
// wakes after a sixteenth of it.
func TestAFileHoldsAGiB(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(1024*mib), DefaultRetention.Bytes)
	assert.Equal(t, DefaultRetention.Bytes/16, DefaultRetention.SweepBytes)
}

// Log text is the cluster's to write, so the queue's worst case is a bound on bytes, never
// on lines: at 16 KiB a line, a bound on lines alone would let it hold a gigabyte.
func TestTheWriteQueueHoldsAtMostSixteenMiB(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(16*mib), int64(DefaultRetention.WriteQueue)*DefaultRetention.WriteBatchBytes)
}

// testRetention is the production setting with no janitor, which a test about
// anything else opens its manager with.
var testRetention = func() Retention {
	r := DefaultRetention
	r.Interval = 0
	return r
}()
