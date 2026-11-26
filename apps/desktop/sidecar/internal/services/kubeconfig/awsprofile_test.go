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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd/api"
)

// execConfigOf is a config whose one context, eks, signs in through exec.
func execConfigOf(exec *api.ExecConfig) *api.Config {
	cfg := api.NewConfig()
	cfg.Contexts["eks"] = &api.Context{Cluster: "c", AuthInfo: "u"}
	cfg.AuthInfos["u"] = &api.AuthInfo{Exec: exec}
	return cfg
}

// The profile is the exec env's AWS_PROFILE, its last one, else the value of
// --profile in either form.
func TestAWSProfileReadsTheExecProvider(t *testing.T) {
	for _, tc := range []struct {
		name string
		exec *api.ExecConfig
		want string
	}{
		{"env", &api.ExecConfig{Env: []api.ExecEnvVar{{Name: "AWS_PROFILE", Value: "prod"}}}, "prod"},
		{"last env", &api.ExecConfig{Env: []api.ExecEnvVar{{Name: "AWS_PROFILE", Value: "a"}, {Name: "AWS_PROFILE", Value: "b"}}}, "b"},
		{"env over args", &api.ExecConfig{
			Env:  []api.ExecEnvVar{{Name: "AWS_PROFILE", Value: "prod"}},
			Args: []string{"--profile", "dev"},
		}, "prod"},
		{"args", &api.ExecConfig{Args: []string{"eks", "get-token", "--cluster-name", "c", "--profile", "dev"}}, "dev"},
		{"args with =", &api.ExecConfig{Args: []string{"eks", "get-token", "--profile=dev"}}, "dev"},
		{"empty env reads args", &api.ExecConfig{
			Env:  []api.ExecEnvVar{{Name: "AWS_PROFILE", Value: ""}},
			Args: []string{"--profile", "dev"},
		}, "dev"},
		{"flag last", &api.ExecConfig{Args: []string{"--profile"}}, ""},
		{"none", &api.ExecConfig{Args: []string{"eks", "get-token"}, Env: []api.ExecEnvVar{{Name: "AWS_REGION", Value: "x"}}}, ""},
	} {
		assert.Equal(t, tc.want, awsProfileOf(execConfigOf(tc.exec), "eks"), tc.name)
	}
}

// A context that is missing, names a missing user, or signs in without exec
// names no profile.
func TestAWSProfileOfNoExecIsNone(t *testing.T) {
	cfg := execConfigOf(nil)
	assert.Empty(t, awsProfileOf(cfg, "eks"))
	assert.Empty(t, awsProfileOf(cfg, "other"))
	cfg.Contexts["lost"] = &api.Context{AuthInfo: "gone"}
	assert.Empty(t, awsProfileOf(cfg, "lost"))
}

// The service reads the profile off its loaded kubeconfig, and none before it
// is read.
func TestTheServiceReadsAContextsAWSProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: https://example.invalid}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [eks, get-token, --cluster-name, c]
      env:
      - {name: AWS_PROFILE, value: prod}
contexts:
- name: eks
  context: {cluster: c, user: u}
`), 0o600))
	svc := newServiceAt(t, path, testInterval)
	assert.Empty(t, svc.AWSProfile("eks"), "not read yet")
	start(t, svc)
	assert.Equal(t, "prod", svc.AWSProfile("eks"))
}
