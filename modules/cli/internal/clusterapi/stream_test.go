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

package clusterapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// fakeClient lets tests drive Stream behavior without HTTP.
type fakeClient struct {
	fetchPages []*LogRecordsQueryResponse
	fetchErrs  []error
	fetchCalls int

	followRecords <-chan logs.LogRecord
	followErrs    <-chan error
	followVars    LogRecordsFollowVars
}

func (f *fakeClient) LogRecordsFetch(ctx context.Context, _ LogRecordsFetchVars) (*LogRecordsQueryResponse, error) {
	i := f.fetchCalls
	f.fetchCalls++
	if i < len(f.fetchErrs) && f.fetchErrs[i] != nil {
		return nil, f.fetchErrs[i]
	}
	if i >= len(f.fetchPages) {
		return &LogRecordsQueryResponse{}, nil
	}
	return f.fetchPages[i], nil
}

func (f *fakeClient) LogRecordsFollow(ctx context.Context, v LogRecordsFollowVars) (<-chan logs.LogRecord, <-chan error) {
	f.followVars = v
	return f.followRecords, f.followErrs
}

func mkRecord(msg string) logs.LogRecord {
	return logs.LogRecord{Timestamp: time.Now(), Message: msg}
}

func TestStream_ImplementsLogStream(t *testing.T) {
	var _ logs.Stream = (*Stream)(nil)
}

func TestStream_PaginateWalksNextCursorUntilNil(t *testing.T) {
	cur := "cursor-1"
	fc := &fakeClient{
		fetchPages: []*LogRecordsQueryResponse{
			{Records: []logs.LogRecord{mkRecord("a"), mkRecord("b")}, NextCursor: &cur},
			{Records: []logs.LogRecord{mkRecord("c")}, NextCursor: nil},
		},
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "HEAD", Sources: []string{"x"}, Paginate: true})

	require.NoError(t, s.Start(context.Background()))
	got := drainAll(t, s, 2*time.Second)
	require.NoError(t, s.Err())
	require.Len(t, got, 3)
	assert.Equal(t, "a", got[0].Message)
	assert.Equal(t, "c", got[2].Message)
	assert.Equal(t, 2, fc.fetchCalls)
}

func TestStream_NoPaginateStopsAfterFirstPage(t *testing.T) {
	// Without Paginate the bootstrap fetch returns whatever the first page
	// holds, even if NextCursor is set — useful for `--head=N` where the
	// caller wants exactly one page.
	cur := "cursor-1"
	fc := &fakeClient{
		fetchPages: []*LogRecordsQueryResponse{
			{Records: []logs.LogRecord{mkRecord("a"), mkRecord("b")}, NextCursor: &cur},
			{Records: []logs.LogRecord{mkRecord("c")}, NextCursor: nil},
		},
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "HEAD", Sources: []string{"x"}, Limit: 2})

	require.NoError(t, s.Start(context.Background()))
	got := drainAll(t, s, 2*time.Second)
	require.NoError(t, s.Err())
	require.Len(t, got, 2, "explicit Limit must cap output to a single page")
	assert.Equal(t, "a", got[0].Message)
	assert.Equal(t, "b", got[1].Message)
	assert.Equal(t, 1, fc.fetchCalls, "explicit Limit must not paginate")
}

func TestStream_TailModeSingleFetch(t *testing.T) {
	fc := &fakeClient{
		fetchPages: []*LogRecordsQueryResponse{
			{Records: []logs.LogRecord{mkRecord("x"), mkRecord("y")}, NextCursor: nil},
		},
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "TAIL", Sources: []string{"x"}, Limit: 10})

	require.NoError(t, s.Start(context.Background()))
	got := drainAll(t, s, 2*time.Second)
	require.NoError(t, s.Err())
	require.Len(t, got, 2)
	assert.Equal(t, 1, fc.fetchCalls, "tail should not paginate")
}

func TestStream_FollowAfterFetch(t *testing.T) {
	rec := make(chan logs.LogRecord, 2)
	errs := make(chan error, 1)
	rec <- mkRecord("live-1")
	rec <- mkRecord("live-2")
	close(rec)
	close(errs)

	fc := &fakeClient{
		fetchPages: []*LogRecordsQueryResponse{
			{Records: []logs.LogRecord{mkRecord("past")}, NextCursor: nil},
		},
		followRecords: rec,
		followErrs:    errs,
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "TAIL", Sources: []string{"x"}, Limit: 10, Follow: true})

	require.NoError(t, s.Start(context.Background()))
	got := drainAll(t, s, 2*time.Second)
	require.NoError(t, s.Err())
	require.Len(t, got, 3)
	assert.Equal(t, "past", got[0].Message)
	assert.Equal(t, "live-2", got[2].Message)
}

func TestStream_FollowOnlyMode_SkipsBootstrapFetch(t *testing.T) {
	rec := make(chan logs.LogRecord, 1)
	errs := make(chan error, 1)
	rec <- mkRecord("live")
	close(rec)
	close(errs)

	fc := &fakeClient{
		fetchPages:    []*LogRecordsQueryResponse{{Records: []logs.LogRecord{mkRecord("backlog")}}},
		followRecords: rec,
		followErrs:    errs,
	}
	s := newStreamForTest(fc, StreamConfig{Sources: []string{"x"}, Follow: true /* Mode left empty */})

	require.NoError(t, s.Start(context.Background()))
	got := drainAll(t, s, 2*time.Second)
	require.NoError(t, s.Err())
	require.Len(t, got, 1)
	assert.Equal(t, "live", got[0].Message)
	assert.Equal(t, 0, fc.fetchCalls, "follow-only mode must not call Fetch")
	// Without an anchor the cluster-api logRecordsFollow subscription replays
	// full history. Stream must default After to "now" when bootstrap is
	// skipped.
	assert.NotEmpty(t, fc.followVars.After, "follow-only mode must anchor After at 'now'")
}

func TestStream_FollowAfterFetch_AnchorsAtBootstrapTimestamp(t *testing.T) {
	bootstrapTs := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)
	rec := make(chan logs.LogRecord, 1)
	errs := make(chan error, 1)
	close(rec)
	close(errs)

	fc := &fakeClient{
		fetchPages: []*LogRecordsQueryResponse{
			{Records: []logs.LogRecord{{Timestamp: bootstrapTs, Message: "past"}}, NextCursor: nil},
		},
		followRecords: rec,
		followErrs:    errs,
	}
	s := newStreamForTest(fc, StreamConfig{Sources: []string{"x"}, Mode: "TAIL", Limit: 1, Follow: true})

	require.NoError(t, s.Start(context.Background()))
	_ = drainAll(t, s, 2*time.Second)
	assert.Equal(t, bootstrapTs.Format(time.RFC3339Nano), fc.followVars.After,
		"follow must resume after the last bootstrap record's timestamp")
}

func TestStream_StartReturnsFirstFetchError(t *testing.T) {
	fc := &fakeClient{
		fetchErrs: []error{errors.New("rbac denied")},
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "TAIL", Sources: []string{"x"}, Limit: 10})

	err := s.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rbac denied")
}

func TestStream_StartSurfacesAPINotInstalled(t *testing.T) {
	fc := &fakeClient{
		fetchErrs: []error{ErrAPINotInstalled},
	}
	s := newStreamForTest(fc, StreamConfig{Mode: "TAIL", Sources: []string{"x"}, Limit: 10})

	err := s.Start(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAPINotInstalled), "Start must propagate ErrAPINotInstalled so callers can fall back")
}

func drainAll(t *testing.T, s *Stream, timeout time.Duration) []logs.LogRecord {
	t.Helper()
	var got []logs.LogRecord
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case rec, ok := <-s.Records():
			if !ok {
				return got
			}
			got = append(got, rec)
		case <-deadline.C:
			t.Fatalf("timed out waiting for records, got %d so far", len(got))
		}
	}
}
