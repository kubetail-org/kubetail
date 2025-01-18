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

package clusterapi

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const DefaultNamespace = "kubetail-system"
const DefaultServiceName = "kubetail-cluster-api"

// Represents connect args
type connectArgs struct {
	Namespace   string
	ServiceName string
	Port        string
}

// Parse connect url and return connect args
func parseConnectUrl(connectUrl string) (*connectArgs, error) {
	u, err := url.Parse(connectUrl)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(u.Hostname(), ".")

	serviceName := parts[0]

	// get namespace
	var namespace string
	if len(parts) > 1 {
		namespace = parts[1]
	} else {
		nsPathname := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
		nsBytes, err := os.ReadFile(nsPathname)
		if err != nil {
			return nil, fmt.Errorf("unable to read current namespace from %s: %v", nsPathname, err)
		}
		namespace = string(nsBytes)
	}

	// get port
	port := u.Port()
	if port == "" {
		port = "50051"
	}

	return &connectArgs{
		Namespace:   namespace,
		ServiceName: serviceName,
		Port:        port,
	}, nil
}
