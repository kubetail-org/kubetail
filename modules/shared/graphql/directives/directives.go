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

package directives

import (
	"context"
	"reflect"

	"github.com/99designs/gqlgen/graphql"
	"github.com/go-playground/validator/v10"

	gqlerrors "github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

var validate *validator.Validate

func init() {
	validate = validator.New()
}

func IsNilPtr(val interface{}) bool {
	return reflect.TypeOf(val).Kind() == reflect.Ptr && reflect.ValueOf(val).IsNil()
}

func ValidateDirective(ctx context.Context, obj interface{}, next graphql.Resolver, rule string, message *string) (interface{}, error) {
	// get val
	val, err := next(ctx)
	if IsNilPtr(val) || err != nil {
		return val, err
	}

	// validate
	err = validate.Var(val, rule)
	if err != nil {
		// init error
		var msg string
		if message != nil {
			msg = *message
		}
		gqlerr := gqlerrors.NewValidationError(rule, msg)

		// add to context
		graphql.AddError(ctx, gqlerr)
	}

	return val, nil
}

// Returns nil if there are errors in the context
func NullIfValidationFailedDirective(ctx context.Context, obj interface{}, next graphql.Resolver) (interface{}, error) {
	errList := graphql.GetErrors(ctx)
	if errList != nil {
		return nil, nil
	}
	return next(ctx)
}
