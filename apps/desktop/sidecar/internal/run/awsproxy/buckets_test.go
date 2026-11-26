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

package awsproxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// listing answers S3's ListBuckets with names, in pages of one, and any other
// request "ok". A nil names fails the list.
func (f *fixture) listing(names func() []string) {
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || !r.URL.Query().Has("max-buckets") {
			_, _ = io.WriteString(w, "ok")
			return
		}
		all := names()
		if all == nil {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "<Error><Code>AccessDenied</Code></Error>")
			return
		}
		i := 0
		if token := r.URL.Query().Get("continuation-token"); token != "" {
			i = int(token[0] - '0')
		}
		page := "<Bucket><Name>" + all[i] + "</Name></Bucket>"
		next := ""
		if i+1 < len(all) {
			next = "<ContinuationToken>" + string(rune('0'+i+1)) + "</ContinuationToken>"
		}
		_, _ = io.WriteString(w, "<ListAllMyBucketsResult><Buckets>"+page+"</Buckets>"+next+"</ListAllMyBucketsResult>")
	}
}

// lists counts the ListBuckets requests the upstream got, and drops the rest.
func (f *fixture) lists() int {
	n := 0
	for {
		select {
		case got := <-f.seen:
			if strings.Contains(got.uri, "max-buckets") {
				n++
				assert.NoError(f.t, got.signedBy)
			}
		default:
			return n
		}
	}
}

// list is the run's listing of an S3 bucket's objects.
func (f *fixture) list(bucket string) *http.Request {
	return f.request(http.MethodGet, "/"+bucket+"?list-type=2", "s3", "")
}

// A bucket the login lists is read unasked; another account's asks, drawn as
// such, and reaches AWS only once approved.
func TestAReadOfAnotherAccountsBucketAsks(t *testing.T) {
	f := newFixture(t)
	f.listing(func() []string { return []string{"mine", "also-mine"} })

	require.Equal(t, http.StatusOK, f.serve(f.list("also-mine")).Code)
	assert.Equal(t, 2, f.lists(), "one list, both its pages")
	assert.Empty(t, f.asker.asked)

	f.asker.approve = false
	for _, r := range []*http.Request{f.list("theirs"), f.request(http.MethodHead, "/theirs/data", "s3", "")} {
		assert.Equal(t, http.StatusForbidden, f.serve(r).Code)
		asked := testutil.Recv(t, f.asker.asked, "the request")
		assert.Equal(t, permissions.Write, asked.Action.AccessLevel)
		assert.Equal(t, "123456789012/eu-west-1", asked.Action.Scope, "the login's account still decides")
		assert.True(t, asked.Action.Once)
		assert.Contains(t, asked.Sent.Details, runproxy.Detail{Label: labelBucketOwner, Value: notThisLogin})
	}
	assert.Zero(t, f.lists(), "the list is kept for a minute")
	f.none()
}

// A data read of another account's bucket asks as a data read, drawn as
// such, with no account.
func TestAForeignBucketsDataReadDrawsItsOwner(t *testing.T) {
	f := newFixture(t)
	f.listing(func() []string { return []string{"mine"} })
	f.asker.approve = false
	f.serve(f.request(http.MethodGet, "/theirs/key", "s3", ""))
	asked := testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, permissions.DataRead, asked.Action.AccessLevel)
	assert.True(t, asked.Action.Once)
	assert.Contains(t, asked.Sent.Details, runproxy.Detail{Label: labelBucketOwner, Value: notThisLogin})
}

// A list that fails owns nothing, so every bucket asks.
func TestAFailedBucketListAsksForEveryBucket(t *testing.T) {
	f := newFixture(t)
	f.listing(func() []string { return nil })
	f.asker.approve = false
	assert.Equal(t, http.StatusForbidden, f.serve(f.list("mine")).Code)
	testutil.Recv(t, f.asker.asked, "the request")
}

// A bucket made during the run is found by a list made a minute later.
func TestABucketMadeInTheRunIsFound(t *testing.T) {
	f := newFixture(t)
	var mu sync.Mutex
	names := []string{"old"}
	f.listing(func() []string { mu.Lock(); defer mu.Unlock(); return names })
	now := time.Now()
	f.buckets.now = func() time.Time { return now }
	f.asker.approve = false

	f.serve(f.list("new"))
	testutil.Recv(t, f.asker.asked, "the request")
	mu.Lock()
	names = []string{"old", "new"}
	mu.Unlock()
	f.serve(f.list("new"))
	testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, 1, f.lists(), "within the minute the list is not made again")

	now = now.Add(time.Minute)
	require.Equal(t, http.StatusOK, f.serve(f.list("new")).Code)
	assert.Empty(t, f.asker.asked)
}

// A bucket a list held is owned only while that list is fresh: once it is
// not, a list that no longer holds the bucket makes its read ask.
func TestAnOwnedBucketExpiresWithItsList(t *testing.T) {
	f := newFixture(t)
	var mu sync.Mutex
	names := []string{"mine"}
	f.listing(func() []string { mu.Lock(); defer mu.Unlock(); return names })
	now := time.Now()
	f.buckets.now = func() time.Time { return now }

	require.Equal(t, http.StatusOK, f.serve(f.list("mine")).Code)
	assert.Equal(t, 1, f.lists())

	mu.Lock()
	names = []string{"other"}
	mu.Unlock()
	now = now.Add(time.Minute)
	f.asker.approve = false
	assert.Equal(t, http.StatusForbidden, f.serve(f.list("mine")).Code)
	testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, 1, f.lists(), "the stale list is made again")
}

// Requests that arrive while the list runs wait for that one list.
func TestConcurrentRequestsShareOneBucketList(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	f.listing(func() []string { <-release; return []string{"mine"} })
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			assert.Equal(t, http.StatusOK, f.serve(f.list("mine")).Code)
		})
	}
	first := testutil.Recv(t, f.seen, "the list")
	assert.Contains(t, first.uri, "max-buckets")
	close(release)
	wg.Wait()
	assert.Zero(t, f.lists(), "no request made a list of its own")
}

// A list AWS does not answer fails, so a bucket read asks.
func TestABucketListThatIsNotAnsweredAsks(t *testing.T) {
	f := newFixture(t)
	f.upstream.Close()
	f.asker.approve = false
	assert.Equal(t, http.StatusForbidden, f.serve(f.list("mine")).Code)
	testutil.Recv(t, f.asker.asked, "the request")
}

// A list whose answer does not parse fails whole, so even a bucket it named
// before breaking off asks.
func TestABucketListThatDoesNotParseAsks(t *testing.T) {
	f := newFixture(t)
	f.answers(http.StatusOK, "<ListAllMyBucketsResult><Buckets><Bucket><Name>mine</Name></Bucket>")
	f.asker.approve = false
	assert.Equal(t, http.StatusForbidden, f.serve(f.list("mine")).Code)
	testutil.Recv(t, f.asker.asked, "the request")
}

// A bucket list AWS answers with a dead credential's code drops the
// credential, as a forwarded request's answer does, and owns nothing.
func TestABucketListWithADeadCredentialDropsIt(t *testing.T) {
	f := newFixture(t)
	f.answers(http.StatusForbidden, "<Error><Code>ExpiredToken</Code></Error>")
	f.asker.approve = false
	assert.Equal(t, http.StatusForbidden, f.serve(f.list("mine")).Code)
	testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, 1, f.id.dropped())
}

// A list that fails because its caller gave up is not kept: the next request
// lists again.
func TestACallerThatGivesUpLeavesNoList(t *testing.T) {
	b := NewBuckets()
	ctx, cancel := context.WithCancel(t.Context())
	var calls atomic.Int32
	list := func(ctx context.Context, _ string, _ aws.Credentials) (map[string]bool, error) {
		if calls.Add(1) == 1 {
			cancel() // the caller gives up while its list runs
			return nil, ctx.Err()
		}
		return map[string]bool{"mine": true}, nil
	}
	assert.False(t, b.owns(ctx, testCreds, "eu-west-1", "mine", list))
	assert.True(t, b.owns(context.Background(), testCreds, "eu-west-1", "mine", list))
	assert.Equal(t, int32(2), calls.Load())
}

// A login AWS holds dead forgets its buckets: the next login may be another
// account's, so the next read of a bucket lists again.
func TestADroppedLoginForgetsItsBuckets(t *testing.T) {
	f := newFixture(t)
	f.answer = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("max-buckets"):
			_, _ = io.WriteString(w, "<ListAllMyBucketsResult><Buckets><Bucket><Name>mine</Name></Bucket></Buckets></ListAllMyBucketsResult>")
		case strings.Contains(r.Host, "ec2"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "<Response><Errors><Error><Code>ExpiredToken</Code></Error></Errors></Response>")
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}
	require.Equal(t, http.StatusOK, f.serve(f.list("mine")).Code)
	assert.Equal(t, 1, f.lists())

	f.serve(f.ec2())
	f.lists()
	require.Equal(t, 1, f.id.dropped())
	require.Equal(t, http.StatusOK, f.serve(f.list("mine")).Code)
	assert.Equal(t, 1, f.lists(), "the list is made again")
}

// With no login the bucket list owns nothing, so the read would ask; it is
// answered as a forward with no login is, before anyone is asked, since its
// account is read from the credentials it would be signed with.
func TestABucketListWithNoLoginIsRefusedUnasked(t *testing.T) {
	f := newFixture(t)
	f.id.err = errors.New("no login")
	w := f.serve(f.list("mine"))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), noLogin)
	assert.Empty(t, f.asker.asked)
	f.none()
}

// A list answers only for the login it was listed with: a profile that became
// another account's, or with no account id took another key, lists again.
func TestAListAnswersOnlyForItsLogin(t *testing.T) {
	b := NewBuckets()
	var listed []string
	list := func(_ context.Context, _ string, creds aws.Credentials) (map[string]bool, error) {
		listed = append(listed, loginOf(creds))
		if creds == testCreds {
			return map[string]bool{"mine": true}, nil
		}
		return map[string]bool{}, nil
	}
	other := testCreds
	other.AccountID = "999999999999"
	noAccount := testCreds
	noAccount.AccountID = ""
	otherKey := noAccount
	otherKey.AccessKeyID = "AKIAOTHERKEYEXAMPLE"

	assert.True(t, b.owns(t.Context(), testCreds, "eu-west-1", "mine", list))
	assert.False(t, b.owns(t.Context(), other, "eu-west-1", "mine", list), "another account")
	assert.False(t, b.owns(t.Context(), noAccount, "eu-west-1", "mine", list), "a login with no account id")
	assert.False(t, b.owns(t.Context(), otherKey, "eu-west-1", "mine", list), "another key with no account id")
	assert.Equal(t, []string{"123456789012", "999999999999", testCreds.AccessKeyID, "AKIAOTHERKEYEXAMPLE"}, listed)
}

// A malformed account id is no account: the login is its access key, as the
// classifier treats it.
func TestALoginWithAMalformedAccountIsItsKey(t *testing.T) {
	malformed := testCreds
	malformed.AccountID = "not-an-account"
	other := malformed
	other.AccessKeyID = "AKIAOTHERKEYEXAMPLE"
	assert.Equal(t, testCreds.AccessKeyID, loginOf(malformed))
	assert.NotEqual(t, loginOf(malformed), loginOf(other))
}

// A request that ends while it waits for another's list owns nothing.
func TestAWaitThatEndsOwnsNothing(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	list := func(context.Context, string, aws.Credentials) (map[string]bool, error) {
		close(started)
		<-release
		return map[string]bool{"mine": true}, nil
	}
	b := NewBuckets()
	done := make(chan bool)
	go func() { done <- b.owns(context.Background(), testCreds, "eu-west-1", "mine", list) }()
	testutil.Wait(t, started, "the list")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.False(t, b.owns(ctx, testCreds, "eu-west-1", "mine", list))
	close(release)
	assert.True(t, testutil.Recv(t, done, "the first request"))
}

// A request that waited for another's list reads that list and makes none.
func TestAWaitingRequestReadsTheListItWaitedFor(t *testing.T) {
	b := NewBuckets()
	// The last list is stale, so every lookup reads the clock and says so.
	now := time.Now()
	b.login, b.listedAt = loginOf(testCreds), now.Add(-relistAfter)
	looked := make(chan struct{}, 8)
	b.now = func() time.Time { looked <- struct{}{}; return now }
	var lists atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	list := func(context.Context, string, aws.Credentials) (map[string]bool, error) {
		lists.Add(1)
		close(started)
		<-release
		return map[string]bool{"mine": true}, nil
	}
	done := make(chan bool, 2)
	owns := func() { done <- b.owns(context.Background(), testCreds, "eu-west-1", "mine", list) }

	go owns()
	testutil.Wait(t, started, "the list")
	for range 2 {
		testutil.Recv(t, looked, "the first request's lookups")
	}
	go owns()
	testutil.Recv(t, looked, "the second request finding the list stale")
	close(release)
	assert.True(t, testutil.Recv(t, done, "one request"))
	assert.True(t, testutil.Recv(t, done, "the other request"))
	assert.EqualValues(t, 1, lists.Load())
}
