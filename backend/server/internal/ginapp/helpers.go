// Copyright 2024 Andres Morey
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

package ginapp

import (
	"html/template"
	"path"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/backend/server/internal/grpchelpers"
	"github.com/kubetail-org/kubetail/backend/server/internal/k8shelpers"
)

const k8sTokenSessionKey = "k8sToken"
const k8sTokenCtxKey = "k8sToken"

func mustConfigureK8S(config Config) *rest.Config {
	opts := k8shelpers.Options{KubeConfig: config.KubeConfig, Mode: k8shelpers.ModeCluster}
	switch config.AuthMode {
	case AuthModeCluster:
		opts.Mode = k8shelpers.ModeCluster
	case AuthModeLocal:
		opts.Mode = k8shelpers.ModeLocal
	default:
		opts.Mode = k8shelpers.ModeToken
	}
	return k8shelpers.MustConfigure(opts)
}

func mustLoadTemplatesWithFuncs(glob string) *template.Template {
	funcMap := template.FuncMap{
		"pathJoin": path.Join,
	}

	tmpl := template.New("").Funcs(funcMap)

	// parse templates from a specified directory or pattern
	parsedTemplates, err := tmpl.ParseGlob(glob)
	if err != nil {
		panic(err)
	}

	return parsedTemplates
}

func mustConnectNATS() *nats.Conn {
	nc, err := nats.Connect("nats://nats:4222/") //nats.DefaultURL)
	if err != nil {
		panic(err)
	}
	return nc
}

func mustNewGcrpConnectionManager() *grpchelpers.ConnectionManager {
	gcm, err := grpchelpers.NewConnectionManager()
	if err != nil {
		panic(err)
	}
	return gcm
}
