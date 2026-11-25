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

package kubeconfig

import (
	"strings"

	"k8s.io/client-go/tools/clientcmd/api"
)

// AWSProfile is the AWS profile contextName's exec provider names, the one
// its credentials plugin reaches AWS as: AWS_PROFILE in its env, else the
// value of --profile in its args. It is "" for none, and before the
// kubeconfig is read.
func (s *Service) AWSProfile(contextName string) string {
	cfg, _ := s.Get()
	return awsProfileOf(cfg, contextName)
}

// awsProfileOf is AWSProfile over one snapshot.
func awsProfileOf(cfg *api.Config, contextName string) string {
	kctx, ok := cfg.Contexts[contextName]
	if !ok {
		return ""
	}
	user, ok := cfg.AuthInfos[kctx.AuthInfo]
	if !ok || user.Exec == nil {
		return ""
	}
	// The plugin's environment is the sidecar's with these appended, so the
	// last one wins.
	profile := ""
	for _, env := range user.Exec.Env {
		if env.Name == "AWS_PROFILE" {
			profile = env.Value
		}
	}
	if profile != "" {
		return profile
	}
	args := user.Exec.Args
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--profile="); ok {
			return value
		}
		if arg == "--profile" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
