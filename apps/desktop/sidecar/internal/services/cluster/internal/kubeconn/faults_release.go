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

//go:build !debug

// A release build injects no faults: these stubs keep the callers' shape and carry nothing
// else. The real ones are in faults_debug.go.
package kubeconn

import (
	"errors"

	"k8s.io/client-go/transport"
)

// Faults is the KUBETAIL_FAULTS rules, which a release build never holds.
type Faults struct{}

// ParseFaults accepts only the empty spec. A release build never reads the variable, so a
// rule reaching here was set by a caller, which is told rather than ignored.
func ParseFaults(spec string) (Faults, error) {
	if spec == "" {
		return Faults{}, nil
	}
	return Faults{}, errors.New("fault injection needs a debug build")
}

// StoreFails reports whether every cache's store should refuse to open.
func (Faults) StoreFails() bool { return false }

// wrapper is nil: a release build adds no layer.
func (Faults) wrapper(string) transport.WrapperFunc { return nil }
