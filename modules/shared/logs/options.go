// Copyright 2024-2025 Andres Morey
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

package logs

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Option func(target any) error

// WithKubeContext sets the kube context of the stream or source watcher
func WithKubeContext(kubeContext string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.kubeContext = kubeContext
		case *sourceWatcher:
			t.kubeContext = kubeContext
		}
		return nil
	}
}

// WithBearerToken sets the bearer token of the source watcher
func WithBearerToken(token string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.bearerToken = token
		}
		return nil
	}
}

// WithSince sets the since time for the stream
func WithSince(ts time.Time) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.sinceTime = ts
		}
		return nil
	}
}

// WithUntil sets the until time for the stream
func WithUntil(ts time.Time) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.untilTime = ts
		}
		return nil
	}
}

// WithFollow sets whether to follow/tail the log stream
func WithFollow(follow bool) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.follow = follow
		}
		return nil
	}
}

// WithHead sets the number of lines to return from the beginning
func WithHead(n int64) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			if n < 0 {
				return fmt.Errorf("head must be >= 0")
			}
			t.mode = streamModeHead
			t.maxNum = n
		}
		return nil
	}
}

// WithTail sets the number of lines to return from the end
func WithTail(n int64) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			if n < 0 {
				return fmt.Errorf("tail must be >= 0")
			}
			t.mode = streamModeTail
			t.maxNum = n
		}
		return nil
	}
}

// WithAll sets whether to return all logs
func WithAll() Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.mode = streamModeAll
		}
		return nil
	}
}

// WithGrep sets the grep filter for the stream
func WithGrep(pattern string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				return nil
			}

			// Replace spaces with ANSI-tolerant pattern
			pattern = strings.ReplaceAll(pattern, " ", `(?:(?:\x1B\[[0-9;]*[mK])?)*\s(?:(?:\x1B\[[0-9;]*[mK])?)*`)

			// Prepend the (?i) flag to make the regex case-insensitive
			pattern = "(?i)" + pattern

			// Compile the regex pattern
			regex, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("invalid grep pattern: %w", err)
			}

			t.grep = pattern
			t.grepRegex = regex
		}
		return nil
	}
}

// WithRegions sets the region filters for the source watcher
func WithRegions(regions []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.regions = regions
		}
		return nil
	}
}

// WithZones sets the zone filters for the source watcher
func WithZones(zones []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.zones = zones
		}
		return nil
	}
}

// WithOSes sets the operating system filters for the source watcher
func WithOSes(oses []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.oses = oses
		}
		return nil
	}
}

// WithArches sets the architecture filters for the source watcher
func WithArches(arches []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.arches = arches
		}
		return nil
	}
}

// WithNodes sets the node filters for the source watcher
func WithNodes(nodes []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.nodes = nodes
		}
		return nil
	}
}

// WithContainers sets the container filters for the source watcher
func WithContainers(containers []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.containers = containers
		}
		return nil
	}
}

// WithLogFetcher sets the log fetcher for the stream
func WithLogFetcher(logFetcher LogFetcher) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.logFetcher = logFetcher
		}
		return nil
	}
}

// WithAllowedNamespaces restricts the allowed namespaces
func WithAllowedNamespaces(allowedNamespaces []string) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.allowedNamespaces = allowedNamespaces
		}
		return nil
	}
}

func WithAllContainers(allContainers bool) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *sourceWatcher:
			t.allContainers = allContainers
		}

		return nil
	}
}

// WithTruncateAtBytes sets the maximum number of bytes
// to read for log messages
func WithTruncateAtBytes(n uint64) Option {
	return func(target any) error {
		switch t := target.(type) {
		case *Stream:
			t.truncatedAtBytes = n
		}
		return nil
	}
}
