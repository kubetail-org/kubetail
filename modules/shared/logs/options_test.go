// Copyright 2024-2026 The Kubetail Authors
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
	"context"
	"fmt"
	"testing"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"
	"github.com/stretchr/testify/mock"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOptions(t *testing.T) {
	opts := []Option{
		WithKubeContext("test"),
		WithRegions([]string{"us-east-1"}),
	}

	// Init connection manager
	cm := &k8shelpersmock.MockConnectionManager{}
	cm.On("GetOrCreateClientset", mock.Anything).Return(&fake.Clientset{}, nil)
	cm.On("GetDefaultNamespace", mock.Anything).Return("default")

	s, err := NewStream(context.Background(), cm, nil, opts...)
	fmt.Println(err)
	fmt.Println(s)
}

// TestWithGrep contains various test scenarios for the WithGrep function.
func TestWithGrep(t *testing.T) {
	// Test Case 1: Basic case-insensitive matching
	t.Run("BasicCaseInsensitiveMatch", func(t *testing.T) {
		stream := &Stream{} // Create a new Stream instance for the test
		err := WithGrep("hello world")(stream)
		if err != nil {
			t.Fatalf("WithGrep returned an unexpected error: %v", err)
		}
		if stream.grepRegex == nil {
			t.Fatal("grepRegex was not set after WithGrep call")
		}

		// Assert that various casing combinations match
		if !stream.grepRegex.MatchString("Hello World") {
			t.Errorf("Expected 'Hello World' to match 'hello world' (case-insensitive), but it didn't")
		}
		if !stream.grepRegex.MatchString("hello world") {
			t.Errorf("Expected 'hello world' to match 'hello world', but it didn't")
		}
		if !stream.grepRegex.MatchString("HELLO WORLD") {
			t.Errorf("Expected 'HELLO WORLD' to match 'hello world', but it didn't")
		}
		// Assert that a non-matching string does not match
		if stream.grepRegex.MatchString("Goodbye World") {
			t.Errorf("Expected 'Goodbye World' not to match 'hello world', but it did")
		}
	})

	// Test Case 2: Pattern with leading/trailing spaces (should be trimmed)
	t.Run("PatternWithLeadingTrailingSpaces", func(t *testing.T) {
		stream := &Stream{}
		err := WithGrep("  trim me  ")(stream) // Pattern with spaces
		if err != nil {
			t.Fatalf("WithGrep returned an unexpected error: %v", err)
		}
		if stream.grepRegex == nil {
			t.Fatal("grepRegex was not set")
		}

		// Assert that the trimmed pattern matches
		if !stream.grepRegex.MatchString("trim me") {
			t.Errorf("Expected 'trim me' to match '  trim me  ', but it didn't")
		}
		if !stream.grepRegex.MatchString("TRIM ME") { // Case-insensitive check
			t.Errorf("Expected 'TRIM ME' to match '  trim me  ', but it didn't")
		}
	})

	// Test Case 3: Pattern with special regex characters (e.g., '.', which matches any character)
	t.Run("SpecialRegexCharacters", func(t *testing.T) {
		stream := &Stream{}
		// The pattern "foo.bar" means "foo" followed by any character, followed by "bar".
		// Our function doesn't escape user input, so '.' is treated as a regex wildcard.
		err := WithGrep("foo.bar")(stream)
		if err != nil {
			t.Fatalf("WithGrep returned an unexpected error: %v", err)
		}
		if stream.grepRegex == nil {
			t.Fatal("grepRegex was not set")
		}

		// Assert that it matches strings where '.' is a wildcard
		if !stream.grepRegex.MatchString("fooXbar") {
			t.Errorf("Expected 'fooXbar' to match 'foo.bar', but it didn't")
		}
		if !stream.grepRegex.MatchString("FOO.BAR") { // Case-insensitive check
			t.Errorf("Expected 'FOO.BAR' to match 'foo.bar', but it didn't")
		}
		if !stream.grepRegex.MatchString("foo.bar") { // Literal match
			t.Errorf("Expected 'foo.bar' to match 'foo.bar', but it didn't")
		}
		// Assert that it does not match if the wildcard condition isn't met
		if stream.grepRegex.MatchString("foobar") {
			t.Errorf("Expected 'foobar' not to match 'foo.bar', but it did")
		}
	})
}
