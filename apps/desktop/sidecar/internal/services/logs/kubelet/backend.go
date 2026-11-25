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

package kubelet

import (
	"context"
	"errors"
	"net/http"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// Log is one container's log as the kubelet serves it: the path it is read at,
// where its pod runs, the runtime id of the instance read, which names the stream
// in each line's cursor, and whether that instance has stopped, so the log never
// grows.
type Log struct {
	Key      logline.StreamKey
	Place    logline.Place
	Instance string
	Finished bool
}

// Backend reads streams' lines, so a read has one shape whoever serves it. A read
// is a stream: the caller pulls its lines one at a time and cancels it once it
// has what it needs, and once its lines close it says how it ended. Each read
// keeps within o's bounds, limits and grep.
type Backend interface {
	// Forward reads oldest first, and with o.Follow keeps reading as the log grows.
	Forward(ctx context.Context, l Log, o Options) *Read
	// Backward reads newest window first, each window oldest first, and ends each
	// window with an item of its own.
	Backward(ctx context.Context, l Log, o Options) *Read
}

// failure is how an error ended one stream's read.
type Failure int

const (
	// failNone is a read that ended clean.
	FailNone Failure = iota
	// failFatal is the cluster refusing the user's credentials, or Kubetail no
	// longer connecting to it: it fails the whole page or follow.
	FailFatal
	// failDenied is a 403: access to pods/log is granted per namespace, so it is
	// this stream's alone.
	FailDenied
	// failEnded is any other status under 500, the server's answer for this log
	// now (a container waiting to start, a pod gone).
	FailEnded
	// failTransient is a 5xx, a 408, a 429 or a transport error, which a follow
	// retries: a request the server timed out or throttled says nothing about
	// the log.
	FailTransient
	// failLimit is a second too loud to read: the stream stops there, and the page
	// is cut at it.
	FailLimit
)

// FailureKind is how err ended a stream's read.
func FailureKind(err error) Failure {
	var se *StatusError
	var loud LoudSecond
	switch {
	case err == nil:
		return FailNone
	case errors.As(err, &loud):
		// Checked first: an error that is not a StatusError is otherwise transient.
		return FailLimit
	case errors.Is(err, ErrRevoked):
		return FailFatal
	case !errors.As(err, &se) || se.Code >= http.StatusInternalServerError,
		se.Code == http.StatusRequestTimeout, se.Code == http.StatusTooManyRequests:
		return FailTransient
	case se.Code == http.StatusUnauthorized:
		return FailFatal
	case se.Code == http.StatusForbidden:
		return FailDenied
	}
	return FailEnded
}

// FatalError is what err fails a whole page or follow with, nil unless it is
// fatal.
func FatalError(err error) error {
	if FailureKind(err) != FailFatal {
		return nil
	}
	if se, ok := errors.AsType[*StatusError](err); ok {
		return refusal.New(refusal.CodeUnauthorized, "the cluster refused to show this log: "+se.Error())
	}
	return ErrRevoked
}
