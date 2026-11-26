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

package bash

import (
	"net"
	"net/url"
	"strconv"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/kubeproxy"
)

// kubeServer is the server every run's kubeconfig names. client-go sends each
// request to proxy-url in absolute form and never resolves this name, and
// .invalid resolves nowhere (RFC 2606). kubectl keys its cache on the server
// URL, so a fixed one keeps the cache warm while the port moves from run to run.
const kubeServer = "http://" + kubeproxy.Host

// kubeUser is the run's one user, which holds no credential: clientcmd
// applies a user's credentials only to a server reached over TLS.
const kubeUser = "kubetail"

// writeKubeconfig writes a run's kubeconfig to path, 0600: one context, named
// context and current, whose cluster is kubeServer dialled through the
// loopback port, the grant's token the proxy-url's password. The name is the
// user's kubeconfig text, so it is written as data by client-go's own encoder,
// never through a template.
func writeKubeconfig(path, context string, port int, token string) error {
	proxy := url.URL{
		Scheme: "http",
		User:   url.UserPassword(kubeproxy.ProxyUser, token),
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
	}
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters[context] = &clientcmdapi.Cluster{Server: kubeServer, ProxyURL: proxy.String()}
	cfg.AuthInfos[kubeUser] = &clientcmdapi.AuthInfo{}
	cfg.Contexts[context] = &clientcmdapi.Context{Cluster: context, AuthInfo: kubeUser}
	cfg.CurrentContext = context
	raw, err := clientcmd.Write(*cfg)
	if err != nil { // coverage:ignore a config of strings always encodes
		return err
	}
	return writeNew(path, raw)
}
