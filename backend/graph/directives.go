package graph

import (
	"context"
	"reflect"

	"github.com/99designs/gqlgen/graphql"
	"github.com/go-playground/validator/v10"

	"github.com/kubetail-org/kubetail/graph/lib"
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
		gqlerr := lib.NewValidationError(rule, msg)

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
