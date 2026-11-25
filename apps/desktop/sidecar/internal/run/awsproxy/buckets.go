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
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// relistAfter is how long a list of the login's buckets is kept before a
// bucket it does not hold lists them again, since a run can make one.
const relistAfter = time.Minute

// Buckets are the login's own buckets, which the proxy lists itself: a read
// naming any other asks, since its owner's log would record it.
type Buckets struct {
	now     func() time.Time
	listing chan struct{} // held by the one request that lists

	mu       sync.Mutex
	names    map[string]bool
	login    string // loginOf the credentials names was listed with
	listedAt time.Time
}

// NewBuckets is a login's buckets, listed when a request first names one.
func NewBuckets() *Buckets {
	return &Buckets{now: time.Now, listing: make(chan struct{}, 1)}
}

// listFunc lists the buckets of the login creds sign as. A list that fails
// owns nothing.
type listFunc func(ctx context.Context, region string, creds aws.Credentials) (map[string]bool, error)

// loginOf names the login creds sign as: its account, or with no account id
// Kubetail can trust, its access key.
func loginOf(creds aws.Credentials) string {
	if account := accountOf(creds); account != "" {
		return account
	}
	return creds.AccessKeyID
}

// owns is whether the login creds sign as owns bucket, calling list when no
// list of that login's is fresh. Only a fresh list answers, a bucket in it
// included: a deleted bucket's name can be taken by another account, and a
// profile can become another account's. One request lists at a time; those
// that arrive meanwhile wait for its list.
func (b *Buckets) owns(ctx context.Context, creds aws.Credentials, region, bucket string, list listFunc) bool {
	login := loginOf(creds)
	if owned, fresh := b.lookup(login, bucket); fresh {
		return owned
	}
	select {
	case b.listing <- struct{}{}:
		defer func() { <-b.listing }()
	case <-ctx.Done():
		return false
	}
	// The request waited on may have listed it.
	if owned, fresh := b.lookup(login, bucket); fresh {
		return owned
	}
	names, err := list(ctx, region, creds)
	if err != nil && ctx.Err() != nil {
		// The caller gave up: its failure says nothing of the login's buckets.
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.names, b.login, b.listedAt = names, login, b.now()
	return names[bucket]
}

// forget drops the list, so the next request that names a bucket lists again.
func (b *Buckets) forget() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.names, b.login, b.listedAt = nil, "", time.Time{}
}

// lookup is whether the last list holds bucket, and whether it is login's
// and fresh.
func (b *Buckets) lookup(login, bucket string) (owned, fresh bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.names[bucket], b.login == login && !b.listedAt.IsZero() && b.now().Sub(b.listedAt) < relistAfter
}

var errBucketList = errors.New("the login's buckets could not be listed")

// listBuckets is S3's ListBuckets, paged to its end: the one request the
// proxy makes that no command sent. It takes no slot of the grant's.
func (p *Proxy) listBuckets(ctx context.Context, region string, creds aws.Credentials) (map[string]bool, error) {
	names := map[string]bool{}
	query := url.Values{"max-buckets": {"1000"}}
	for {
		resp, err := p.send(ctx, creds, outbound{
			method: http.MethodGet, host: services["s3"].host(region), target: "/?" + query.Encode(),
			service: "s3", region: region, header: http.Header{},
		})
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_, _ = p.readError(resp)
			resp.Body.Close()
			return nil, errBucketList
		}
		var page struct {
			Names []string `xml:"Buckets>Bucket>Name"`
			Next  string   `xml:"ContinuationToken"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, errBucketList
		}
		for _, name := range page.Names {
			names[name] = true
		}
		if page.Next == "" {
			return names, nil
		}
		query.Set("continuation-token", page.Next)
	}
}
