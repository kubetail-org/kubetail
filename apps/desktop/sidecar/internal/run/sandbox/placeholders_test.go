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

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A region is AWS's commercial partition alone, in AWS's own form.
func TestAWSRegion(t *testing.T) {
	t.Parallel()

	for _, region := range []string{"us-east-1", "eu-west-1", "ap-southeast-5", "il-central-1"} {
		assert.True(t, AWSRegion(region), region)
	}
	for _, region := range []string{
		"", "cn-north-1", "us-gov-west-1", "eusc-de-east-1", "us-iso-east-1", "us-isob-east-1",
		"us-isof-south-1", "eu-isoe-west-1", "us-east-1.evil.com", "US-EAST-1",
		"us-east", "us-east-10", "us-east-1/x",
	} {
		assert.False(t, AWSRegion(region), region)
	}
}

// A profile is a name that cannot end its section header or start another.
func TestAWSProfile(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"default", "prod", "my-org_dev.admin", "a@b", "team/role", "acct:123", "x+y", "_x"} {
		assert.True(t, AWSProfile(profile), profile)
	}
	for _, profile := range []string{
		"", "a b", "a]\n[default", "a]", "[a", "a\nb", "a;b", "a#b", "a=b", "-a", ".a", strings.Repeat("a", 129),
	} {
		assert.False(t, AWSProfile(profile), profile)
	}
}

// awsRun is a run holding the AWS mount's variables, with its run directory
// and relay port; extra replaces or adds rows.
func awsRun(t *testing.T, extra ...string) Run {
	kubetail := t.TempDir()
	dir := filepath.Join(kubetail, "run")
	require.NoError(t, os.Mkdir(dir, 0o700))
	env := map[string]string{
		"AWS_ENDPOINT_URL":          "http://127.0.0.1:4100",
		"AWS_ACCESS_KEY_ID":         AWSKeyIDPrefix + strings.Repeat("AB", 32),
		"AWS_SECRET_ACCESS_KEY":     AWSSecretPlaceholder,
		"AWS_REGION":                "eu-west-1",
		"AWS_DEFAULT_REGION":        "eu-west-1",
		"AWS_EC2_METADATA_DISABLED": "true",
		"AWS_CONFIG_FILE":           filepath.Join(dir, "aws", "config"),
	}
	for _, kv := range extra {
		name, value, _ := strings.Cut(kv, "=")
		env[name] = value
	}
	r := Run{Env: []string{"PATH=/usr/bin"}}
	for name, value := range env {
		r.Env = append(r.Env, name+"="+value)
	}
	r.Policy.Always.Kubetail, r.Policy.Always.Read = []string{kubetail}, []string{dir}
	r.Policy.Network.Relays = []Relay{{Port: 4100, Socket: filepath.Join(dir, "proxy.sock")}}
	return r
}

// A run holding the mount's values starts; a real credential, another host,
// another port, another region or a file elsewhere does not, whoever put it
// there, and no other AWS name passes at all.
func TestOnlyAPlaceholderPassesTheCheck(t *testing.T) {
	t.Parallel()

	assert.NoError(t, checked(awsRun(t)))
	for _, kv := range []string{
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_ACCESS_KEY_ID=" + AWSKeyIDPrefix + "ABAB",
		"AWS_ACCESS_KEY_ID=" + AWSKeyIDPrefix + strings.Repeat("ab", 32),
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"AWS_ENDPOINT_URL=http://evil.example:4100",
		"AWS_ENDPOINT_URL=http://127.0.0.1:4200",
		"AWS_ENDPOINT_URL=https://127.0.0.1:4100",
		"AWS_REGION=cn-north-1",
		"AWS_DEFAULT_REGION=",
		"AWS_EC2_METADATA_DISABLED=false",
		"AWS_CONFIG_FILE=/home/u/.aws/config",
		"AWS_CONFIG_FILE=" + filepath.Join(t.TempDir(), "aws", "config"),
		"AWS_PROFILE=prod",
		"AWS_SESSION_TOKEN=t",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI=http://169.254.170.2/x",
		"AWS_ENDPOINT_URL_S3=http://127.0.0.1:4100",
	} {
		assert.ErrorContains(t, checked(awsRun(t, kv)), "may not pass", kv)
	}
}

// Unpassable names every placeholder still, so a caller that builds no mount,
// the sandboxed login shell among them, passes none of them.
func TestEveryAWSNameIsUnpassable(t *testing.T) {
	t.Parallel()

	for name := range placeholders {
		assert.True(t, Unpassable(name), name)
	}
}
