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
	"context"
	"fmt"
)

// Stamp marks chunks read now, which keeps them out of eviction for ReadTTL. A chunk
// stamped within a tenth of ReadTTL is left alone, and a page whose chunks all were
// takes no write at all, so a viewer sitting on one chunk costs a write every tenth
// of ReadTTL rather than one a page.
func (s *Store) Stamp(ctx context.Context, chunks []Chunk) error {
	now, every := s.m.now().UnixMilli(), (s.m.ret.ReadTTL / 10).Milliseconds()
	var ids []int64
	for _, c := range chunks {
		if c.ReadAt <= now-every {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	err := s.with(ctx, func(ctx context.Context, st stmts) error {
		_, err := st.Exec(ctx, stmtStampChunks, now, idList(ids), every)
		return err
	})
	if err != nil {
		return fmt.Errorf("stamp: %w", err)
	}
	return nil
}
