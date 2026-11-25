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

package bash

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// awsTestCreds are the user's credentials, which no run may hold.
var awsTestCreds = aws.Credentials{
	AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMIK7MDENGbPxRfiCYzzKEY",
	SessionToken: "FwoGZXIvYXdzEXAMPLESESSIONTOKENxyz",
}

// fakeAWSLogin is the user's AWS login for a test: known credentials and a
// region.
type fakeAWSLogin struct{ region string }

func (l fakeAWSLogin) Credentials(context.Context) (aws.Credentials, error) { return awsTestCreds, nil }
func (fakeAWSLogin) Drop()                                                  {}
func (l fakeAWSLogin) Region() string                                       { return l.region }

// awsAs is AWS where no kube-context names a profile, so every run reaches
// it as login.
func awsAs(login AWSLogin, client *http.Client) CloudPlatform {
	return NewAWS(func(string) string { return "" }, func(string) AWSLogin { return login }, client)
}

// awsMount mounts AWS on a fresh run directory at port 4100.
func awsMount(t *testing.T, region string) (CloudMount, CloudRun) {
	run := CloudRun{On: switchedOn, Port: 4100, Dir: t.TempDir(), Session: session.Session{Kind: session.Chat}}
	m, err := awsAs(fakeAWSLogin{region}, nil).Mount(t.Context(), run)
	require.NoError(t, err)
	t.Cleanup(m.Grant.End)
	return m, run
}

// The mount sets its variables and no other, every value its own, and leaves
// the region out when the sandbox would refuse it.
func TestTheAWSMountSetsExactlyItsVariables(t *testing.T) {
	m, run := awsMount(t, "eu-west-1")
	assert.Equal(t, []string{
		"AWS_ENDPOINT_URL=http://127.0.0.1:4100",
		"AWS_ACCESS_KEY_ID=" + sandbox.AWSKeyIDPrefix + strings.ToUpper(m.Grant.Token()),
		"AWS_SECRET_ACCESS_KEY=" + sandbox.AWSSecretPlaceholder,
		"AWS_EC2_METADATA_DISABLED=true",
		"AWS_CONFIG_FILE=" + filepath.Join(run.Dir, "aws", "config"),
		"AWS_REGION=eu-west-1",
		"AWS_DEFAULT_REGION=eu-west-1",
	}, m.Env)
	assertNoUserCanSet(t, m.Env)
	assert.Equal(t, permissions.AWS, m.Grant.Target())
	require.Len(t, m.Routes, 1)
	assert.Equal(t, "127.0.0.1:"+strconv.Itoa(run.Port), m.Routes[0].Host)

	for _, region := range []string{"", "cn-north-1"} {
		m, _ := awsMount(t, region)
		assert.NotContains(t, strings.Join(m.Env, "\n"), "REGION=", region)
	}
}

// With no profile, the config file is the default section's settings alone,
// under the run directory.
func TestTheAWSConfigFileIsTheDefaultSection(t *testing.T) {
	_, run := awsMount(t, "eu-west-1")
	text, err := os.ReadFile(filepath.Join(run.Dir, "aws", "config"))
	require.NoError(t, err)
	assert.Equal(t, "[default]\ndisable_request_compression = true\n", string(text))
}

// The run's profile is declared with the run's keys, which --profile reads
// in place of the environment's; the default profile's keys join its one
// section.
func TestTheAWSConfigFileDeclaresTheProfile(t *testing.T) {
	keys := "aws_access_key_id = KUBETAILAB\naws_secret_access_key = " + sandbox.AWSSecretPlaceholder + "\n"
	assert.Equal(t, "[default]\ndisable_request_compression = true\n[profile prod]\n"+keys+
		"disable_request_compression = true\n", awsConfig("prod", "KUBETAILAB"))
	assert.Equal(t, "[default]\n"+keys+"disable_request_compression = true\n", awsConfig("default", "KUBETAILAB"))
}

// A run on a cluster whose kube-context names a profile reaches AWS as that
// profile, in its region, and declares it; one with no cluster, a context
// naming none, or a name the file cannot hold reaches AWS as the default
// chain. Each profile's login is made once.
func TestTheAWSMountFollowsTheClustersProfile(t *testing.T) {
	profiles := map[string]string{"eks-prod": "prod", "eks-odd": "a]\n[x"}
	regions := map[string]string{"prod": "eu-west-1"}
	var made []string
	platform := NewAWS(
		func(kubeContext string) string { return profiles[kubeContext] },
		func(profile string) AWSLogin {
			made = append(made, profile)
			return fakeAWSLogin{regions[profile]}
		}, nil)
	mount := func(kubeContext string) (CloudMount, string) {
		run := CloudRun{On: switchedOn, Port: 4100, Dir: t.TempDir(), KubeContext: kubeContext, Session: session.Session{Kind: session.Chat}}
		m, err := platform.Mount(t.Context(), run)
		require.NoError(t, err)
		t.Cleanup(m.Grant.End)
		text, err := os.ReadFile(filepath.Join(run.Dir, "aws", "config"))
		require.NoError(t, err)
		return m, string(text)
	}

	for range 2 {
		m, text := mount("eks-prod")
		assert.Contains(t, m.Env, "AWS_REGION=eu-west-1")
		assert.Equal(t, awsConfig("prod", sandbox.AWSKeyIDPrefix+strings.ToUpper(m.Grant.Token())), text)
	}
	for _, kubeContext := range []string{"", "eks-plain", "eks-odd"} {
		m, text := mount(kubeContext)
		assert.NotContains(t, strings.Join(m.Env, "\n"), "REGION=", kubeContext)
		assert.Equal(t, awsConfig("", ""), text, kubeContext)
	}
	assert.Equal(t, []string{"prod", ""}, made)
}

// Each profile keeps one login and one bucket list, which its runs share.
func TestEachProfileHasItsOwnLoginAndBuckets(t *testing.T) {
	p := NewAWS(
		func(kubeContext string) string { return kubeContext },
		func(profile string) AWSLogin { return fakeAWSLogin{profile} }, nil).(*awsPlatform)
	of := func(kubeContext string) awsProfile {
		_, profile := p.profileFor(CloudRun{KubeContext: kubeContext})
		return profile
	}
	prod, dev, none := of("prod"), of("dev"), of("")
	assert.Equal(t, prod, of("prod"))
	assert.Equal(t, []string{"prod", "dev", ""}, []string{prod.login.Region(), dev.login.Region(), none.login.Region()})
	assert.Same(t, prod.buckets, of("prod").buckets)
	assert.NotSame(t, prod.buckets, dev.buckets)
	assert.NotSame(t, prod.buckets, none.buckets)
}

// A run directory that already holds an aws folder fails the mount.
func TestAnAWSFolderAlreadyThereFailsTheMount(t *testing.T) {
	run := CloudRun{On: switchedOn, Port: 4100, Dir: t.TempDir()}
	require.NoError(t, os.Mkdir(filepath.Join(run.Dir, "aws"), 0o700))
	_, err := awsAs(fakeAWSLogin{"eu-west-1"}, nil).Mount(t.Context(), run)
	assert.ErrorIs(t, err, fs.ErrExist)
}

// hungLogin is a login whose region read waits on release, as one reading the
// user's AWS config from a dead network mount would.
type hungLogin struct {
	fakeAWSLogin
	release chan struct{}
}

func (l hungLogin) Region() string {
	<-l.release
	return "eu-west-1"
}

// A mount whose region read hangs gives up when its context ends, having
// written nothing.
func TestAMountAbandonsAHungRegionRead(t *testing.T) {
	login := hungLogin{release: make(chan struct{})}
	t.Cleanup(func() { close(login.release) })
	run := CloudRun{On: switchedOn, Port: 4100, Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	mounted := make(chan error, 1)
	go func() {
		_, err := awsAs(login, nil).Mount(ctx, run)
		mounted <- err
	}()
	assert.ErrorIs(t, testutil.Recv(t, mounted, "the mount giving up"), context.Canceled)
	assert.NoDirExists(t, filepath.Join(run.Dir, "aws"))
}

// The client AWS is reached through waits for an answer's headers no longer
// than awsAnswerTimeout, and leaves a stored encoding as stored.
func TestTheAWSClientBoundsAnAnswersWait(t *testing.T) {
	transport := awsAs(fakeAWSLogin{}, nil).(*awsPlatform).client.Transport.(*http.Transport)
	assert.Equal(t, awsAnswerTimeout, transport.ResponseHeaderTimeout)
	assert.True(t, transport.DisableCompression)
}
