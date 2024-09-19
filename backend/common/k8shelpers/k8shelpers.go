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

package k8shelpers

import (
	"context"
	"errors"
	"os"
	"regexp"

	zlog "github.com/rs/zerolog/log"
	authv1 "k8s.io/api/authentication/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubetail-org/kubetail/backend/common/config"
)

type Options struct {
	AuthMode   config.AuthMode
	KubeConfig string
}

// Configure kubernetes or die
func MustConfigure(cfg *config.Config) *rest.Config {
	k8sCfg, err := Configure(cfg)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}
	return k8sCfg
}

// Configure kubernetes
func Configure(cfg *config.Config) (*rest.Config, error) {
	switch cfg.AuthMode {
	case config.AuthModeCluster:
		return configureCluster()
	case config.AuthModeLocal:
		return configureLocal(cfg.KubeConfig)
	case config.AuthModeToken:
		k8sCfg, err := configureCluster()
		if err == nil {
			return k8sCfg, nil
		}
		return configureLocal(cfg.KubeConfig)
	default:
		panic("not implemented")
	}
}

// Configure client for use inside cluster
func configureCluster() (*rest.Config, error) {
	return rest.InClusterConfig()
}

// Configure client using local kubectl
func configureLocal(file string) (*rest.Config, error) {
	cfgBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return clientcmd.RESTConfigFromKubeConfig(cfgBytes)
}

type HelperService struct {
	cfg      *rest.Config
	authMode config.AuthMode
}

// Adapted from https://github.com/kubernetes/dashboard/blob/b231dc2b89dbdfe325dc433dd7cc83abca6ddfea/modules/api/pkg/client/manager.go#L228
func (s *HelperService) HasAccess(token string) (string, error) {
	cfg := rest.CopyConfig(s.cfg)

	// handle token-mode
	if s.authMode == config.AuthModeToken {
		// exit if token is blank
		if token == "" {
			return "", errors.New("token required")
		}

		cfg.BearerToken = token
		cfg.BearerTokenFile = ""
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", err
	}

	switch s.authMode {
	case config.AuthModeCluster, config.AuthModeLocal:
		// check access by trying to get server version
		discoveryClient := clientset.Discovery()
		_, err = discoveryClient.ServerVersion()
		return string(s.authMode), err
	case config.AuthModeToken:
		// use token service
		tokenReview := &authv1.TokenReview{
			Spec: authv1.TokenReviewSpec{
				Token: token,
			},
		}

		result, err := clientset.AuthenticationV1().TokenReviews().Create(context.Background(), tokenReview, metav1.CreateOptions{})
		if err != nil {
			if k8serrors.IsForbidden(err) {
				return getUsernameFromError(err), nil
			}
			return "", err
		}

		return getUsername(result.Status.User.Username), nil
	default:
		panic("not implemented")
	}
}

func NewHelperService(cfg *rest.Config, authMode config.AuthMode) *HelperService {
	return &HelperService{cfg, authMode}
}

func getUsername(name string) string {
	const groups = 5
	const nameGroupIdx = 4
	re := regexp.MustCompile(`(?P<ignore>[\w-]+):(?P<type>[\w-]+):(?P<namespace>[\w-_]+):(?P<name>[\w-]+)`)
	match := re.FindStringSubmatch(name)

	if match == nil || len(match) != groups {
		return name
	}

	return match[nameGroupIdx]
}

func getUsernameFromError(err error) string {
	re := regexp.MustCompile(`^.* User "(.*)" cannot .*$`)
	return re.ReplaceAllString(err.Error(), "$1")
}
