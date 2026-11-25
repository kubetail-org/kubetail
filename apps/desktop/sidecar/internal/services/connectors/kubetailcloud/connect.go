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

package kubetailcloud

import (
	"context"
	"fmt"
)

// Connect ends an attempt under way, then signs in fresh, since this sign-in
// is how a user changes accounts. It returns once the browser opens and errs
// when it could not; a later failure is the state's. It is refused while
// Restoring and while connected.
func (s *Service) Connect(ctx context.Context) error {
	s.connecting.Lock()
	defer s.connecting.Unlock()
	s.engine.Cancel()
	failure, err := s.engine.Start(ctx, true)
	if err != nil {
		return err
	}
	if failure != "" {
		return fmt.Errorf("kubetail: the sign-in could not start: %s", failure)
	}
	return nil
}
