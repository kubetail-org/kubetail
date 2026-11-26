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

package awsproxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Every row's host and signing region come from the table and the scope's
// region alone.
func TestEachRowNamesItsHost(t *testing.T) {
	for name, want := range map[string][2]string{
		"sts":                  {"sts.eu-west-1.amazonaws.com", "eu-west-1"},
		"ec2":                  {"ec2.eu-west-1.amazonaws.com", "eu-west-1"},
		"eks":                  {"eks.eu-west-1.amazonaws.com", "eu-west-1"},
		"elasticloadbalancing": {"elasticloadbalancing.eu-west-1.amazonaws.com", "eu-west-1"},
		"autoscaling":          {"autoscaling.eu-west-1.amazonaws.com", "eu-west-1"},
		"iam":                  {"iam.amazonaws.com", "us-east-1"},
		"s3":                   {"s3.eu-west-1.amazonaws.com", "eu-west-1"},
		"logs":                 {"logs.eu-west-1.amazonaws.com", "eu-west-1"},
		"monitoring":           {"monitoring.eu-west-1.amazonaws.com", "eu-west-1"},
		"route53":              {"route53.amazonaws.com", "us-east-1"},
		"cloudfront":           {"cloudfront.amazonaws.com", "us-east-1"},
		"ecr":                  {"api.ecr.eu-west-1.amazonaws.com", "eu-west-1"},
		"cloudformation":       {"cloudformation.eu-west-1.amazonaws.com", "eu-west-1"},
	} {
		row, ok := services[name]
		if assert.True(t, ok, name) {
			assert.Equal(t, want[0], row.host("eu-west-1"), name)
			assert.Equal(t, want[1], row.signingRegion("eu-west-1"), name)
		}
	}
	assert.Len(t, services, 13)
}
