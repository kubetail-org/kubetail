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
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublic(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "::1",
		"10.1.2.3", "172.16.0.1", "192.168.1.1", "fd00::1",
		"169.254.169.254", "fe80::1",
		"224.0.0.1", "ff02::1",
		"0.0.0.0", "::", "0.1.2.3",
		"100.64.0.1", "192.0.0.1", "198.18.0.1", "240.0.0.1", "255.255.255.255",
		"::ffff:127.0.0.1",
		"64:ff9b::a9fe:a9fe", // NAT64 carrying 169.254.169.254
		"2002:7f00:1::",      // 6to4 carrying 127.0.0.1
		"::127.0.0.1",        // IPv4-compatible
	} {
		assert.False(t, Public(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{
		"93.184.215.14",
		"2606:4700::1111",
		"64:ff9b::5db8:d70e", // NAT64 carrying 93.184.215.14
		"2002:5db8:d70e::",   // 6to4 carrying 93.184.215.14
	} {
		assert.True(t, Public(netip.MustParseAddr(s)), s)
	}
}
