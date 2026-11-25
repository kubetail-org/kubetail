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

package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/grpc/pokepb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

// pokeServer implements pokepb.PokeServiceServer; a nil broadcaster degrades to
// Unavailable.
type pokeServer struct {
	pokepb.UnimplementedPokeServiceServer
	pokeSvc *poke.Service
}

// Poke fans a SourceHost resync out to every in-process subscriber — the host calls it on
// OS resume / network-on. Best-effort: Poke never blocks, so the response returns as soon
// as the signal is queued.
func (s *pokeServer) Poke(_ context.Context, _ *pokepb.PokeRequest) (*pokepb.PokeResponse, error) {
	if s.pokeSvc == nil {
		return nil, status.Error(codes.Unavailable, "no poke service")
	}
	s.pokeSvc.Poke(poke.SourceHost)
	return &pokepb.PokeResponse{}, nil
}
