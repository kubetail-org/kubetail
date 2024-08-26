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

	"google.golang.org/grpc"
)

type ClientConnInterface interface {
	grpc.ClientConnInterface
	Close() error
	CanonicalTarget() string
}

type ConnectionManagerInterface interface {
	Doit(fn func(conn ClientConnInterface)) (func() error, error)
	Start(ctx context.Context)
	Get(nodeName string) ClientConnInterface
	GetAll() map[string]ClientConnInterface
	Teardown()
}
