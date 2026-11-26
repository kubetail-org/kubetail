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
	"net"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// maxURL is the longest URL a fetch takes, as url.URL.String spells it.
const maxURL = 8192

// refusal is why a fetch will not go, in the words the model reads.
type refusal string

func (r refusal) Error() string { return string(r) }

const (
	errScheme   refusal = "WebFetch fetches http and https URLs only."
	errUserinfo refusal = "WebFetch sends no credentials in a URL."
	errHostname refusal = "WebFetch cannot read this hostname."
	errLong     refusal = "WebFetch takes a URL of at most 8,192 bytes."
	errDotless  refusal = "WebFetch does not fetch localhost or other hostnames without a dot."
	errPrivate  refusal = "WebFetch does not reach local or private addresses."
	errNumeric  refusal = "WebFetch does not read a hostname ending in a number. Call again with the IP address written in full."
)

// target is the URL a fetch of raw requests, or the refusal the model reads.
// Every check here reads the URL alone; the address a name resolves to is the
// dial's to check.
func target(raw string) (*url.URL, error) {
	u, err := rebuild(raw, false)
	if err != nil {
		return nil, err
	}
	if len(u.String()) > maxURL {
		return nil, errLong
	}
	host := u.Hostname()
	if a, err := netip.ParseAddr(host); err == nil {
		if !Public(a) {
			return nil, errPrivate
		}
		return u, nil
	}
	host = strings.TrimSuffix(host, ".")
	if !strings.Contains(host, ".") {
		return nil, errDotless
	}
	if endsInANumber(host) {
		return nil, errNumeric
	}
	return u, nil
}

// rebuild is raw as it is fetched: http or https with a host and no userinfo,
// the host in ASCII, upgraded to https. lenient keeps a host idna rejects as it
// was spelled, for ActionOf, so an idna update changes no stored call's action.
func rebuild(raw string, lenient bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return nil, errScheme
	}
	if u.User != nil {
		return nil, errUserinfo
	}
	// idna refuses the colons of every IPv6 address, so a literal is kept as it is.
	if _, err := netip.ParseAddr(u.Hostname()); err != nil {
		ascii, err := idna.Lookup.ToASCII(u.Hostname())
		switch {
		case err == nil && u.Port() != "":
			u.Host = net.JoinHostPort(ascii, u.Port())
		case err == nil:
			u.Host = ascii
		case !lenient:
			return nil, errHostname
		}
	}
	if u.Scheme == "http" {
		u.Scheme = "https"
		// :80 named http's port; kept, it would send TLS to it.
		if u.Port() == "80" {
			u.Host = strings.TrimSuffix(u.Host, ":80")
		}
	}
	return u, nil
}

// endsInANumber is WHATWG's check on a host's last label: all digits, or 0x
// then hex digits. A resolver or a proxy may read such a host as a shorthand
// IPv4 address (127.1, 0x7f.1) that netip.ParseAddr does not.
func endsInANumber(host string) bool {
	label := host[strings.LastIndex(host, ".")+1:]
	if rest, ok := strings.CutPrefix(strings.ToLower(label), "0x"); ok {
		return strings.Trim(rest, "0123456789abcdef") == ""
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}
