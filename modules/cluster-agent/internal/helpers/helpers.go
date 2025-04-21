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

package helpers

import (
	"context"
	"errors"
	"math/rand"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubetail-org/kubetail/modules/shared/grpchelpers"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// Check if client has required pods/log permissions for given namespace+verb
func CheckPermission(ctx context.Context, clientset kubernetes.Interface, namespaces []string, verb string) error {
	// Ensure token is present
	if _, ok := ctx.Value(grpchelpers.K8STokenCtxKey).(string); !ok {
		return status.Errorf(codes.Unauthenticated, "missing token")
	}

	// ensure namespaces argument is present
	if len(namespaces) < 1 {
		return errors.New("namespaces required")
	}

	// check each namespace individually
	for _, namespace := range namespaces {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Namespace: namespace,
					Group:     "",
					Verb:      verb,
					Resource:  "pods/log",
				},
			},
		}

		if result, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{}); err != nil {
			return err
		} else if !result.Status.Allowed {
			fmt := "permission denied: `%s pods/log` in namespace `%s`"
			return status.Errorf(codes.Unauthenticated, fmt, verb, namespace)
		}
	}

	return nil
}
