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

import "net/netip"

// refused is every IPv4 range netip.Addr's own tests do not name.
var refused = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // this network
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, broadcast included
}

// The IPv6 ranges that carry an IPv4 address, judged by the one they carry.
var (
	nat64      = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour  = netip.MustParsePrefix("2002::/16")
	compatible = netip.MustParsePrefix("::/96")
)

// Public reports whether a fetch may dial a.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	if a.Is4() {
		for _, p := range refused {
			if p.Contains(a) {
				return false
			}
		}
		return true
	}
	b := a.As16()
	switch {
	case nat64.Contains(a), compatible.Contains(a):
		return Public(netip.AddrFrom4([4]byte(b[12:16])))
	case sixToFour.Contains(a):
		return Public(netip.AddrFrom4([4]byte(b[2:6])))
	}
	return true
}
