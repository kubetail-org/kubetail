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

package formerrors

import (
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

type FormErrors map[string]string

// Retrieve error message for a given field name or return an empty string if not present
func (fe FormErrors) Get(field string) string {
	if val, ok := fe[field]; ok {
		return val
	}
	return ""
}

// Initialize a new FormErrors instance
func New(formPtr interface{}, err error) FormErrors {
	// initialize FormErrors instance
	formErrors := make(FormErrors)

	// get form type
	formType := reflect.TypeOf(formPtr).Elem()

	// extract ValidationErrors
	ve := err.(validator.ValidationErrors)

	// iterate through FieldErrors
	for i := 0; i < len(ve); i++ {
		// get error message from `errors_{tag}`
		fe := ve[i]
		structFieldName := fe.StructField()
		structField, _ := formType.FieldByName(structFieldName)
		structTagName := "errors_" + strings.ToLower(fe.Tag())
		formErrors[structFieldName] = structField.Tag.Get(structTagName)
	}

	return formErrors
}
