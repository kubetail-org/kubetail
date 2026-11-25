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

//go:build unix

package awsauth

import (
	"context"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// profile is a service over a config file whose one profile resolves through
// credential_process: a script that counts its runs in a file and answers a
// secret ending in that count, already expired so each read runs it again.
// fail makes it exit 1.
func profile(t *testing.T, region string, fail bool) (*Service, string) {
	dir := t.TempDir()
	count := filepath.Join(dir, "runs")
	script := filepath.Join(dir, "creds.sh")
	exit := ""
	if fail {
		exit = "echo 'sso token expired at /home/u/.aws/sso/cache/x.json' >&2; exit 1\n"
	}
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+exit+
		"id="+rand.Text()+"\n"+
		"echo x >> "+count+"\nn=$(wc -l < "+count+" | tr -d ' ')\n"+
		`printf '{"Version":1,"AccessKeyId":"ASIAIOSFODNN7EXAMPLE","SecretAccessKey":"Xq7Lm2Rv9Tw4Ns8Kd3Pz'"$id"'%s","SessionToken":"Fq9Gh2Jk7Lm3Np8Qr4St'"$id"'%s","Expiration":"2000-01-01T00:00:00Z"}' "$n" "$n"`+"\n"), 0o700))
	cfg := filepath.Join(dir, "config")
	text := "[profile kubetail-test]\ncredential_process = " + script + "\n"
	if region != "" {
		text += "region = " + region + "\n"
	}
	require.NoError(t, os.WriteFile(cfg, []byte(text), 0o600))
	s := newService("", func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx, config.WithSharedConfigFiles([]string{cfg}),
			config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "none")}),
			config.WithSharedConfigProfile("kubetail-test"))
	})
	t.Cleanup(s.Drop)
	return s, count
}

func runs(t *testing.T, count string) int {
	b, err := os.ReadFile(count)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(b), "\n")
}

// Building the service and reading its region runs nothing.
func TestNothingResolvesUntilAsked(t *testing.T) {
	s, count := profile(t, "eu-west-1", false)
	assert.Equal(t, "eu-west-1", s.Region())
	assert.Zero(t, runs(t, count))

	_, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, runs(t, count))
}

// A profile with no region answers "".
func TestNoRegionIsEmpty(t *testing.T) {
	s, _ := profile(t, "", false)
	assert.Empty(t, s.Region())
}

// A held secret key and session token are blanked in rendered text, and
// forgotten once a refresh replaces them or Drop empties the cache.
func TestAHeldCredentialIsBlankedAndADroppedOneIsForgotten(t *testing.T) {
	s, _ := profile(t, "eu-west-1", false)
	first, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.True(t, safe.HasSecret(first.SecretAccessKey))
	assert.True(t, safe.HasSecret(first.SessionToken))

	second, err := s.Credentials(context.Background())
	require.NoError(t, err)
	require.NotEqual(t, first.SecretAccessKey, second.SecretAccessKey, "the expired credential refreshes")
	assert.False(t, safe.HasSecret(first.SecretAccessKey))
	assert.False(t, safe.HasSecret(first.SessionToken))
	assert.True(t, safe.HasSecret(second.SecretAccessKey))

	s.Drop()
	assert.False(t, safe.HasSecret(second.SecretAccessKey))
	assert.False(t, safe.HasSecret(second.SessionToken))
}

// A chain that does not resolve is one fixed sentence, never the SDK's text.
func TestAFailedChainIsOneSentence(t *testing.T) {
	s, _ := profile(t, "eu-west-1", true)
	_, err := s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)
}

// A configuration that does not load, or names no chain, is no login.
func TestAConfigurationThatDoesNotLoadIsNoLogin(t *testing.T) {
	s := newService("", func(context.Context) (aws.Config, error) { return aws.Config{}, errors.New("bad file") })
	assert.Empty(t, s.Region())
	_, err := s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)
	s.Drop()
}

// gated is a credential provider each Retrieve of which waits on release, and
// answers a secret ending in its count.
type gated struct {
	entered chan struct{}
	release chan struct{}
	n       atomic.Int32
}

func (g *gated) Retrieve(context.Context) (aws.Credentials, error) {
	g.entered <- struct{}{}
	<-g.release
	n := strconv.Itoa(int(g.n.Add(1)))
	return aws.Credentials{AccessKeyID: "ASIAIOSFODNN7EXAMPLE", SecretAccessKey: "Gq4Hk8Lm2Np6Rs9Tv3Wx" + rand.Text() + n}, nil
}

// A Drop that comes while a credential is being retrieved waits for it, then
// forgets it with the cache: a Drop never forgets a credential the cache still
// holds.
func TestDropWaitsForARetrievalInFlight(t *testing.T) {
	g := &gated{entered: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	s := newService("", func(context.Context) (aws.Config, error) {
		return aws.Config{Credentials: aws.NewCredentialsCache(g)}, nil
	})
	t.Cleanup(s.Drop)

	got := make(chan aws.Credentials, 1)
	go func() {
		creds, _ := s.Credentials(context.Background())
		got <- creds
	}()
	testutil.Recv(t, g.entered, "the retrieval")
	dropped := make(chan struct{})
	go func() {
		s.Drop()
		close(dropped)
	}()
	// A negative assertion: a Drop that did not wait would return at once.
	testutil.NoRecv(t, dropped, 50*time.Millisecond, "Drop while a retrieval is in flight")
	g.release <- struct{}{}
	first := testutil.Recv(t, got, "the credential")
	testutil.Wait(t, dropped, "Drop")
	assert.False(t, safe.HasSecret(first.SecretAccessKey), "the dropped credential is forgotten")

	g.release <- struct{}{}
	second, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, first.SecretAccessKey, second.SecretAccessKey, "the cache was emptied")
	assert.True(t, safe.HasSecret(second.SecretAccessKey))
}

// A retrieval whose request ends while it waits its turn is no login.
func TestARetrievalThatEndsWaitingIsNoLogin(t *testing.T) {
	g := &gated{entered: make(chan struct{}, 1), release: make(chan struct{}, 1)}
	s := newService("", func(context.Context) (aws.Config, error) {
		return aws.Config{Credentials: aws.NewCredentialsCache(g)}, nil
	})
	go func() { _, _ = s.Credentials(context.Background()) }()
	testutil.Recv(t, g.entered, "the first retrieval")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.Credentials(ctx)
	assert.ErrorIs(t, err, ErrNoLogin)
	g.release <- struct{}{}
	t.Cleanup(s.Drop)
}

// A retrieval its caller gave up on still holds the turn until the refresh it
// started ends and is registered, so a Drop after it empties the cache of
// what that refresh filled in.
func TestACancelledRetrievalHoldsItsTurnUntilTheRefreshEnds(t *testing.T) {
	g := &gated{entered: make(chan struct{}, 2), release: make(chan struct{}, 2)}
	s := newService("", func(context.Context) (aws.Config, error) {
		return aws.Config{Credentials: aws.NewCredentialsCache(g)}, nil
	})
	t.Cleanup(s.Drop)

	ctx, cancel := context.WithCancel(t.Context())
	gaveUp := make(chan error, 1)
	go func() {
		_, err := s.Credentials(ctx)
		gaveUp <- err
	}()
	testutil.Recv(t, g.entered, "the refresh")
	cancel()
	assert.ErrorIs(t, testutil.Recv(t, gaveUp, "the caller giving up"), ErrNoLogin)

	dropped := make(chan struct{})
	go func() {
		s.Drop()
		close(dropped)
	}()
	// A negative assertion: a Drop the refresh did not hold back would return
	// at once.
	testutil.NoRecv(t, dropped, 50*time.Millisecond, "Drop while the refresh runs")
	g.release <- struct{}{}
	testutil.Wait(t, dropped, "Drop")

	g.release <- struct{}{}
	creds, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), g.n.Load(), "the Drop emptied what the refresh filled in")
	assert.True(t, safe.HasSecret(creds.SecretAccessKey))
}

// A configuration that does not load is read again on the next request, so a
// profile the user fixes is used without a restart.
func TestAConfigurationIsReadAgainUntilItLoads(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "creds.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+
		`printf '{"Version":1,"AccessKeyId":"ASIAIOSFODNN7EXAMPLE","SecretAccessKey":"Hr5Jm9Kp3Lq7Ns2Tw6Vy`+rand.Text()+`"}'`+"\n"), 0o700))
	cfg := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(cfg, []byte("[profile other]\n"), 0o600))
	s := newService("", func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx, config.WithSharedConfigFiles([]string{cfg}),
			config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "none")}),
			config.WithSharedConfigProfile("kubetail-test"))
	})
	t.Cleanup(s.Drop)

	_, err := s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin, "the profile is missing")

	require.NoError(t, os.WriteFile(cfg, []byte("[profile kubetail-test]\ncredential_process = "+script+"\nregion = eu-west-1\n"), 0o600))
	creds, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ASIAIOSFODNN7EXAMPLE", creds.AccessKeyID)
	assert.Equal(t, "eu-west-1", s.Region())
}

// files is a service over an empty config and the credentials file it
// answers, in a fresh directory, the profile the default one.
func files(t *testing.T) (*Service, string) {
	dir := t.TempDir()
	creds := filepath.Join(dir, "credentials")
	s := newService("", func(ctx context.Context) (aws.Config, error) {
		return config.LoadDefaultConfig(ctx, config.WithSharedConfigFiles([]string{filepath.Join(dir, "config")}),
			config.WithSharedCredentialsFiles([]string{creds}))
	})
	t.Cleanup(s.Drop)
	return s, creds
}

// With no default profile the configuration still loads, its chain the EC2
// metadata provider, which never resolves off EC2; the default profile the
// user then creates is read on the next request. The test sets the process's
// environment, so it does not run in parallel.
func TestAProfileCreatedAfterNoLoginIsRead(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	s, creds := files(t)

	_, err := s.Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)

	key := "Jt6Kr0Ms4Nv8Px2Qz5Rb" + rand.Text()
	require.NoError(t, os.WriteFile(creds, []byte("[default]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\naws_secret_access_key = "+key+"\n"), 0o600))
	got, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, key, got.SecretAccessKey)
}

// Keys read from the credentials file are fixed at load: once AWS says they
// are dead, the keys the user wrote in their place are read on the next
// request.
func TestReplacedFileKeysAreReadAfterADrop(t *testing.T) {
	s, creds := files(t)
	write := func(key string) {
		require.NoError(t, os.WriteFile(creds, []byte("[default]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\naws_secret_access_key = "+key+"\n"), 0o600))
	}
	revoked, replaced := "Lu7Mw1Nx5Py9Qa3Rc6Sd"+rand.Text(), "Nv8Px2Qz5Rb9Sc3Td6Ue"+rand.Text()
	write(revoked)
	got, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, revoked, got.SecretAccessKey)

	write(replaced)
	s.Drop()
	got, err = s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, replaced, got.SecretAccessKey)
	assert.False(t, safe.HasSecret(revoked))
	assert.True(t, safe.HasSecret(replaced))
}

// A refresh that never ends gives up the turn at its deadline and unloads its
// chain: a Drop then returns, and the next request loads the configuration
// again.
func TestAHungRefreshGivesUpItsTurn(t *testing.T) {
	g := &gated{entered: make(chan struct{}, 1), release: make(chan struct{}, 1)}
	key := "Pw3Rx7Sz1Tb5Uc9Vd2We" + rand.Text()
	var loads atomic.Int32
	s := newService("", func(context.Context) (aws.Config, error) {
		if loads.Add(1) == 1 {
			return aws.Config{Credentials: aws.NewCredentialsCache(g)}, nil
		}
		return aws.Config{Credentials: aws.NewCredentialsCache(aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "ASIAIOSFODNN7EXAMPLE", SecretAccessKey: key}, nil
		}))}, nil
	})
	s.retrieveTimeout = 50 * time.Millisecond
	t.Cleanup(s.Drop)
	// Cleanups run last first: the hung refresh is let go before the Drop.
	t.Cleanup(func() { g.release <- struct{}{} })

	gaveUp := make(chan error, 1)
	go func() {
		_, err := s.Credentials(context.Background())
		gaveUp <- err
	}()
	assert.ErrorIs(t, testutil.Recv(t, gaveUp, "the retrieval's deadline"), ErrNoLogin)
	testutil.WaitReturn(t, s.Drop, "Drop after the hung refresh")
	creds, err := s.Credentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, key, creds.SecretAccessKey)
}

// A load that fails keeps no region from an earlier one.
func TestAFailedLoadKeepsNoRegion(t *testing.T) {
	var loads atomic.Int32
	s := newService("", func(context.Context) (aws.Config, error) {
		if loads.Add(1) == 1 {
			return aws.Config{Region: "eu-west-1"}, nil
		}
		return aws.Config{}, errors.New("bad file")
	})
	assert.Equal(t, "eu-west-1", s.Region())
	assert.Empty(t, s.Region())
}

// A named profile resolves as itself, with no AWS_PROFILE in the environment
// and no default profile; the default chain then finds no login. The test
// sets the process's environment, so it does not run in parallel.
func TestALoginReachesAWSAsItsProfile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	keys := map[string]string{"prod": "Pr7Kd2Lm9Nq4Rs8Tv3Wx" + rand.Text(), "dev": "Dv4Jk8Ln2Mp6Qr0St5Uw" + rand.Text()}
	require.NoError(t, os.WriteFile(cfg, []byte(
		"[profile prod]\naws_access_key_id = AKIAPRODEXAMPLE\naws_secret_access_key = "+keys["prod"]+"\nregion = eu-west-1\n"+
			"[profile dev]\naws_access_key_id = AKIADEVEXAMPLE\naws_secret_access_key = "+keys["dev"]+"\nregion = us-east-2\n"), 0o600))
	t.Setenv("AWS_CONFIG_FILE", cfg)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "none"))
	for _, name := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION", "AWS_DEFAULT_REGION"} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	for profile, region := range map[string]string{"prod": "eu-west-1", "dev": "us-east-2"} {
		s := New(profile)
		t.Cleanup(s.Drop)
		creds, err := s.Credentials(context.Background())
		require.NoError(t, err, profile)
		assert.Equal(t, keys[profile], creds.SecretAccessKey, profile)
		assert.Equal(t, region, s.Region(), profile)
	}

	_, err := New("").Credentials(context.Background())
	assert.ErrorIs(t, err, ErrNoLogin)
	_, err = New("gone").Credentials(context.Background())
	assert.EqualError(t, err, "no AWS login was found for the profile gone")
}

// The refusal names the profile tried, and how to sign in only for SSO.
func TestNoLoginNamesTheProfileTried(t *testing.T) {
	for _, tc := range []struct {
		err  NoLoginError
		want string
	}{
		{NoLoginError{}, "no AWS login was found"},
		{NoLoginError{SSO: true}, "no AWS login was found; run `aws sso login` outside the sandbox"},
		{NoLoginError{Profile: "prod"}, "no AWS login was found for the profile prod"},
		{NoLoginError{Profile: "prod", SSO: true}, "no AWS login was found for the profile prod; run `aws sso login --profile prod` outside the sandbox"},
	} {
		assert.EqualError(t, &tc.err, tc.want)
		assert.Equal(t, tc.want, tc.err.Refusal())
		assert.ErrorIs(t, &tc.err, ErrNoLogin)
	}
}

// An SSO profile with no session signed in fails as SSO, through its
// session's profile or a role chained from it.
func TestAnSSOProfileWithNoSessionSaysToSignIn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no SSO token cache
	cfg := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(cfg, []byte("[profile dev]\nsso_session = s\nsso_account_id = 123456789012\n"+
		"sso_role_name = r\nregion = eu-west-1\n"+
		"[profile role]\nrole_arn = arn:aws:iam::123456789012:role/r\nsource_profile = dev\n"+
		"[sso-session s]\nsso_start_url = https://example.invalid/start\nsso_region = eu-west-1\n"), 0o600))
	for _, profile := range []string{"dev", "role"} {
		s := newService(profile, func(ctx context.Context) (aws.Config, error) {
			return config.LoadDefaultConfig(ctx, config.WithSharedConfigFiles([]string{cfg}),
				config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "none")}),
				config.WithSharedConfigProfile(profile))
		})
		_, err := s.Credentials(context.Background())
		assert.EqualError(t, err, "no AWS login was found for the profile "+profile+
			"; run `aws sso login --profile "+profile+"` outside the sandbox")
	}
}

// The chain's own requests name Kubetail in the SDK's User-Agent: here STS's,
// for a role assumed from a profile's keys, over TLS the profile's CA bundle
// trusts, so the SDK's own client sends it. The test sets the process's
// environment, so it does not run in parallel.
func TestACredentialRequestNamesKubetail(t *testing.T) {
	clearAWSEnv(t)
	agents := make(chan string, 8)
	sts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(sts.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: sts.Certificate().Raw}), 0o600))
	cfg := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(cfg, []byte("[profile keys]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\n"+
		"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\n"+
		"[profile role]\nrole_arn = arn:aws:iam::123456789012:role/r\nsource_profile = keys\n"+
		"region = eu-west-1\nca_bundle = "+ca+"\nservices = fake\n"+
		"[services fake]\nsts =\n  endpoint_url = "+sts.URL+"\n"), 0o600))
	s := newService("role", func(ctx context.Context) (aws.Config, error) {
		return loadConfig(ctx, "role", config.WithSharedConfigFiles([]string{cfg}),
			config.WithSharedCredentialsFiles([]string{filepath.Join(dir, "none")}))
	})
	t.Cleanup(s.Drop)

	_, err := s.Credentials(context.Background())
	require.ErrorIs(t, err, ErrNoLogin)
	ua := testutil.Recv(t, agents, "the STS request")
	assert.True(t, strings.HasPrefix(ua, "aws-sdk-go-v2/"), ua)
	assert.Contains(t, strings.Fields(ua), "kubetail-desktop/"+version.Version)
}

// clearAWSEnv unsets every AWS_ variable for the test, so only its own files
// steer the SDK: an endpoint, CA bundle or key in the environment would win
// over them. The SDK reads some by presence, so an empty value is not enough.
func clearAWSEnv(t *testing.T) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "AWS_") {
			t.Setenv(name, "") // restores the variable when the test ends
			require.NoError(t, os.Unsetenv(name))
		}
	}
}
