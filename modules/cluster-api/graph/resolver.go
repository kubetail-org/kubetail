// Copyright 2024-2025 Andres Morey
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

package graph

import (
	"context"
	"fmt"
	"strings"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

//go:generate go run github.com/99designs/gqlgen generate

type Resolver struct {
	cm                k8shelpers.ConnectionManager
	grpcDispatcher    *grpcdispatcher.Dispatcher
	allowedNamespaces []string
}

func (r *Resolver) getBearerTokenRequired(ctx context.Context) (string, error) {
	var token string
	if tokenValue, ok := ctx.Value(k8shelpers.K8STokenCtxKey).(string); ok {
		token = strings.TrimSpace(tokenValue)
	}

	if token == "" {
		return "", fmt.Errorf("token required")
	}

	return token, nil
}
