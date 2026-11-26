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

package webfetch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each URL target refuses by name answers its own message.
func TestTargetRefusesByName(t *testing.T) {
	for raw, want := range map[string]error{
		"ftp://a.test/":           errScheme,
		"https:a.test":            errScheme,
		"https:///path":           errScheme,
		"https://user:pw@a.test/": errUserinfo,
		"https://a\u200d.test/":   errHostname,
		"https://a.test/" + strings.Repeat("x", 8192): errLong,
		"https://localhost/":                          errDotless,
		"https://localhost./":                         errDotless,
		"https://intranet/":                           errDotless,
		"https://127.0.0.1/":                          errPrivate,
		"http://[::1]/":                               errPrivate,
		"https://[fe80::1%25eth0]/":                   errPrivate,
		"https://169.254.169.254/latest/":             errPrivate,
		"https://127.1/":                              errNumeric,
		"https://0x7f.1/":                             errNumeric,
		"https://a.123/":                              errNumeric,
		"https://a.0x/":                               errNumeric,
		"https://1.2.3.4./":                           errNumeric,
	} {
		_, err := target(raw)
		assert.Equal(t, want, err, raw)
	}
}

// What target passes is the URL fetched: https, its host in ASCII.
func TestTargetRebuildsTheURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://a.test/x?y=1":        "https://a.test/x?y=1",
		"http://a.test/":              "https://a.test/",
		"http://a.test:80/":           "https://a.test/",
		"http://a.test:8080/":         "https://a.test:8080/",
		"HTTPS://A.Test/":             "https://a.test/",
		"https://münchen.de/":         "https://xn--mnchen-3ya.de/",
		"https://[2606:4700::1111]/":  "https://[2606:4700::1111]/",
		"https://93.184.215.14:8443/": "https://93.184.215.14:8443/",
	} {
		u, err := target(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, u.String(), raw)
	}
}

func TestActionOfReadsTheURLAndItsHost(t *testing.T) {
	for raw, want := range map[string][2]string{
		"http://a.test/x":            {"https://a.test/x", "a.test"},
		"http://a.test:80/":          {"https://a.test/", "a.test"},
		"https://a.test:443/":        {"https://a.test:443/", "a.test"},
		"https://a.test:8443/":       {"https://a.test:8443/", "a.test:8443"},
		"https://münchen.de/":        {"https://xn--mnchen-3ya.de/", "xn--mnchen-3ya.de"},
		"https://[2606:4700::1111]/": {"https://[2606:4700::1111]/", "[2606:4700::1111]"},
		"https://127.0.0.1/":         {"https://127.0.0.1/", "127.0.0.1"},
		"https://localhost/":         {"https://localhost/", "localhost"},
		"https://a\u200d.test/":      {"https://a%E2%80%8D.test/", "a\u200d.test"},
	} {
		got, err := ActionOf(call(raw), "")
		require.NoError(t, err, raw)
		require.NotNil(t, got.Fetch, raw)
		assert.Equal(t, want[0], got.Fetch.URL, raw)
		assert.Equal(t, want[1], got.Fetch.Host, raw)
	}
	for _, raw := range []string{"ftp://a.test/", "https://user@a.test/"} {
		_, err := ActionOf(call(raw), "")
		assert.Error(t, err, raw)
	}
	_, err := ActionOf([]byte(`{}`), "")
	assert.Error(t, err)
}
