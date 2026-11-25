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

// Package model holds the gqlgen-generated GraphQL models (models_gen.go).
//
// This hand-written file exists ONLY to keep the package non-empty: gqlgen rewrites
// models_gen.go on every run, and a package with no Go files can't be loaded for
// autobinding. Types bind directly to the cluster types (see gqlgen.yml), so there are no
// hand-written models.
package model
