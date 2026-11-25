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

package supervisor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A key states the name↔type pairing once, beside the job, where a bare read restates it at every
// read site.
func TestAKeyReadsTheObservableItNames(t *testing.T) {
	e, p, _ := single(t, Succeeded())
	p.commits("v1")
	e.Add(subj)
	e.settle()
	runNext(t, e)
	conn := NewKey[string]("conn")

	read, _ := e.Read(subj)
	o := conn.From(read)

	assert.Equal(t, "conn", conn.Name())
	assert.Equal(t, "v1", o.Value)
	assert.True(t, o.Known())
}
