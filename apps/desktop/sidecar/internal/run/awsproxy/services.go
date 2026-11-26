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

import "strings"

// service is one row of the table: a service a run reaches, how its requests
// name their operation, where they go and the operations its row lists beyond
// the classifier's general rules. Adding a row is a security change.
type service struct {
	// protocol is where the operation is named, which is where AWS reads it.
	protocol protocol
	// errors is how the service's clients read an error, so a refusal is
	// printed as the proxy wrote it.
	errors errorShape
	// hostFormat is the upstream host, "{region}" standing for the scope's.
	// One without it is global, signed in us-east-1.
	hostFormat string
	reads      []string // read beyond the Describe, Get and List prefixes
	destroys   []string // destructive beyond the Delete and Terminate prefixes; an access control change on iam
	secrets    []string // a data read: the answer holds stored data or a secret
	refused    []string // the answer is a credential
}

// protocol is how a service's requests name their operation.
type protocol int

const (
	protocolQuery  protocol = iota // the Action parameter, in the query or a form body; the zero value
	protocolJSON                   // X-Amz-Target
	protocolEither                 // either, but never both
	protocolREST                   // the method and path
)

// errorShape is the body an error answer takes, as a service's clients parse
// it.
type errorShape int

const (
	errorsQuery errorShape = iota // <ErrorResponse><Error>; the zero value
	errorsEC2                     // EC2's <Response><Errors><Error>
	errorsXML                     // the REST XML services' <Error>
	errorsJSON                    // a JSON body, its type also in X-Amzn-Errortype
)

// services is the table, by signing name. A name in a list may hold one "*",
// which matches any run of characters.
var services = map[string]service{
	"sts": {hostFormat: "sts.{region}.amazonaws.com"},
	"ec2": {
		errors:     errorsEC2,
		hostFormat: "ec2.{region}.amazonaws.com",
		secrets: []string{
			"DescribeLaunchTemplateVersions", "GetLaunchTemplateData", "DescribeSpotInstanceRequests",
			"DescribeSpotFleetRequests", "GetConsoleOutput", "GetConsoleScreenshot", "DescribeVpnConnections",
			"GetVpnConnectionDeviceSampleConfiguration",
		},
		refused: []string{"CreateKeyPair", "GetPasswordData"},
	},
	"eks":                  {protocol: protocolREST, errors: errorsJSON, hostFormat: "eks.{region}.amazonaws.com"},
	"elasticloadbalancing": {hostFormat: "elasticloadbalancing.{region}.amazonaws.com"},
	"autoscaling": {
		hostFormat: "autoscaling.{region}.amazonaws.com",
		secrets:    []string{"DescribeLaunchConfigurations"},
	},
	"iam": {
		hostFormat: "iam.amazonaws.com",
		reads:      []string{"Generate*Report", "Simulate*Policy"},
		destroys: []string{
			"Attach*Policy", "Put*Policy", "CreatePolicyVersion", "SetDefaultPolicyVersion",
			"UpdateAssumeRolePolicy", "AddUserToGroup", "CreateLoginProfile", "UpdateLoginProfile",
		},
		refused: []string{
			"CreateAccessKey", "CreateServiceSpecificCredential", "ResetServiceSpecificCredential",
			"CreateVirtualMFADevice",
		},
	},
	"s3": {protocol: protocolREST, errors: errorsXML, hostFormat: "s3.{region}.amazonaws.com"},
	"logs": {
		errors:     errorsJSON,
		protocol:   protocolJSON,
		hostFormat: "logs.{region}.amazonaws.com",
		reads:      []string{"FilterLogEvents", "StartQuery", "StopQuery", "TestMetricFilter"},
		// A query's results are unmasked when its query was, which its id
		// does not show.
		secrets: []string{"GetQueryResults"},
	},
	"monitoring": {protocol: protocolEither, hostFormat: "monitoring.{region}.amazonaws.com"},
	"route53":    {protocol: protocolREST, errors: errorsXML, hostFormat: "route53.amazonaws.com"},
	"cloudfront": {protocol: protocolREST, errors: errorsXML, hostFormat: "cloudfront.amazonaws.com"},
	"ecr": {
		errors:     errorsJSON,
		protocol:   protocolJSON,
		hostFormat: "api.ecr.{region}.amazonaws.com",
		reads:      []string{"BatchGetImage", "BatchCheckLayerAvailability"},
		refused:    []string{"GetAuthorizationToken", "GetDownloadUrlForLayer"},
	},
	"cloudformation": {
		hostFormat: "cloudformation.{region}.amazonaws.com",
		reads:      []string{"ValidateTemplate", "EstimateTemplateCost"},
		secrets: []string{
			"GetTemplate", "GetTemplateSummary", "DescribeStacks", "DescribeStackSet", "DescribeChangeSet",
			"DescribeStackEvents",
		},
	},
}

// host is the row's upstream host for region.
func (s service) host(region string) string {
	return strings.ReplaceAll(s.hostFormat, "{region}", region)
}

// signingRegion is the region a request to the row is signed in: a global
// row's, one host for every region, is us-east-1.
func (s service) signingRegion(region string) string {
	if !strings.Contains(s.hostFormat, "{region}") {
		return "us-east-1"
	}
	return region
}

// listed is whether op is named by one of names, a name holding one "*"
// matching any run of characters there.
func listed(names []string, op string) bool {
	for _, name := range names {
		prefix, suffix, wild := strings.Cut(name, "*")
		if name == op || wild && len(op) >= len(prefix)+len(suffix) &&
			strings.HasPrefix(op, prefix) && strings.HasSuffix(op, suffix) {
			return true
		}
	}
	return false
}
