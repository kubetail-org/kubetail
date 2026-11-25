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
	"cmp"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// call is one request for the classifier: a query call's form, a JSON call's
// target and body, or a REST method and path.
type call struct {
	service, method, path string
	form, target, body    string
}

func (c call) classify() operation {
	method, body := c.method, c.body
	if method == "" {
		method = http.MethodPost
	}
	r := httptest.NewRequest(method, cmp.Or(c.path, "/"), nil)
	if c.form != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
		body = c.form
	}
	if c.target != "" {
		r.Header.Set("X-Amz-Target", c.target)
	}
	return classify(c.service, r, []byte(body))
}

const (
	read      = permissions.Read
	change    = permissions.Write
	destroy   = permissions.Destructive
	perms     = permissions.AccessControl
	secret    = permissions.DataRead
	refusedOp = permissions.AccessLevel("refused")
)

// Every operation this spec names, and each prefix, lands in its level.
func TestEveryRowsOperationsAreClassed(t *testing.T) {
	for want, calls := range map[permissions.AccessLevel][]call{
		read: {
			{service: "sts", form: "Action=GetCallerIdentity&Version=2011-06-15"},
			{service: "ec2", form: "Action=DescribeInstances"},
			{service: "ec2", form: "Action=DescribeInstanceAttribute&Attribute=instanceType"},
			{service: "iam", form: "Action=ListRoles"},
			{service: "iam", form: "Action=GenerateCredentialReport"},
			{service: "iam", form: "Action=SimulatePrincipalPolicy"},
			{service: "ec2", method: "GET", path: "/?Action=DescribeRegions"},
			{service: "logs", target: "Logs_20140328.FilterLogEvents", body: `{"logGroupName":"g"}`},
			{service: "logs", target: "Logs_20140328.StartQuery", body: `{"queryString":"fields @message"}`},
			{service: "monitoring", target: "GraniteServiceVersion20100801.GetMetricData", body: `{}`},
			{service: "monitoring", form: "Action=ListMetrics"},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.BatchGetImage", body: `{"repositoryName":"r"}`},
			{service: "cloudformation", form: "Action=ValidateTemplate&TemplateBody=x"},
			{service: "cloudformation", form: "Action=ListStacks"},
			{service: "s3", method: "GET", path: "/"},
			{service: "s3", method: "GET", path: "/bucket?list-type=2"},
			{service: "s3", method: "HEAD", path: "/bucket/key"},
			{service: "s3", method: "GET", path: "/bucket/key?tagging"},
			{service: "s3", method: "GET", path: "/bucket/key?uploadId=u"},
			{service: "eks", method: "GET", path: "/clusters"},
			{service: "route53", method: "GET", path: "/2013-04-01/hostedzone"},
		},
		change: {
			{service: "ec2", form: "Action=StopInstances&InstanceId.1=i-1"},
			{service: "ec2", method: "GET", path: "/"},
			{service: "ec2", form: "Version=2016-11-15"},
			{service: "ec2", form: "Action=DescribeInstances&x=%zz"},
			{service: "logs", body: `{}`},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.BatchGetImage", body: `{"registryId":"999999999999","repositoryName":"r"}`},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.DescribeImages", body: `{"registryId":"9"}`},
			{service: "cloudformation", form: "Action=ValidateTemplate&TemplateURL=https://b.s3.amazonaws.com/t"},
			{service: "cloudformation", form: "Action=EstimateTemplateCost&TemplateURL=https://b.s3.amazonaws.com/t"},
			{service: "s3", method: "PUT", path: "/bucket/key"},
			{service: "eks", method: "POST", path: "/clusters"},
		},
		destroy: {
			{service: "ec2", form: "Action=TerminateInstances"},
			{service: "ec2", form: "Action=DeleteVpc"},
			{service: "logs", target: "Logs_20140328.DeleteLogGroup", body: `{}`},
			{service: "s3", method: "DELETE", path: "/bucket/key"},
			{service: "s3", method: "POST", path: "/bucket?delete"},
			{service: "eks", method: "DELETE", path: "/clusters/c"},
		},
		perms: {
			{service: "iam", form: "Action=AttachRolePolicy"},
			{service: "iam", form: "Action=PutUserPolicy"},
			{service: "iam", form: "Action=UpdateAssumeRolePolicy"},
			{service: "iam", form: "Action=CreateLoginProfile"},
			{service: "iam", form: "Action=CreateRole"},
			{service: "iam", form: "Action=DeleteRole"},
			{service: "iam", form: "Action=TagRole"},
			{service: "logs", target: "Logs_20140328.PutResourcePolicy", body: `{}`},
			{service: "logs", target: "Logs_20140328.DeleteResourcePolicy", body: `{}`},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.SetRepositoryPolicy", body: `{}`},
			{service: "ec2", form: "Action=CreateNetworkInterfacePermission"},
			{service: "ec2", form: "Action=CreateCapacityReservationGrant"},
		},
		secret: {
			{service: "s3", method: "GET", path: "/bucket/key"},
			{service: "s3", method: "GET", path: "/bucket/a/b?versionId=v"},
			{service: "s3", method: "POST", path: "/bucket/key?select&select-type=2"},
			{service: "ec2", form: "Action=DescribeInstanceAttribute&Attribute=userData"},
			{service: "ec2", form: "Action=DescribeInstanceAttribute"},
			{service: "ec2", form: "Action=DescribeInstanceAttribute&Attribute=kernel&Attribute=userData"},
			{service: "ec2", form: "Action=DescribeLaunchTemplateVersions"},
			{service: "ec2", form: "Action=GetConsoleOutput"},
			{service: "ec2", form: "Action=DescribeVpnConnections"},
			{service: "ec2", form: "Action=GetVpnConnectionDeviceSampleConfiguration"},
			{service: "autoscaling", form: "Action=DescribeLaunchConfigurations"},
			{service: "cloudformation", form: "Action=DescribeStacks"},
			{service: "cloudformation", form: "Action=GetTemplate"},
			{service: "logs", target: "Logs_20140328.GetLogEvents", body: `{"unmask":true}`},
			{service: "logs", target: "Logs_20140328.FilterLogEvents", body: `{"unmask":false,"unmask":true}`},
			{service: "logs", target: "Logs_20140328.StartQuery", body: `{"queryString":"fields unmask(@message)"}`},
			{service: "logs", target: "Logs_20140328.StartQuery", body: `{"queryString":"fields UNMASK (@message)"}`},
			{service: "logs", target: "Logs_20140328.StartQuery", body: `{"queryLanguage":"SQL","queryString":"SELECT Unmask(x)"}`},
			{service: "logs", target: "Logs_20140328.GetLogRecord", body: `not json`},
			{service: "logs", target: "Logs_20140328.GetLogEvents", body: `{"logGroupName":`},
			{service: "logs", target: "Logs_20140328.GetQueryResults", body: `{"queryId":"q"}`},
		},
		refusedOp: {
			{service: "sts", form: "Action=AssumeRole"},
			{service: "sts", form: "Action=GetSessionToken"},
			{service: "ec2", form: "Action=CreateKeyPair"},
			{service: "ec2", form: "Action=GetPasswordData"},
			{service: "iam", form: "Action=CreateAccessKey"},
			{service: "iam", form: "Action=CreateVirtualMFADevice"},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken", body: `{}`},
			{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.GetDownloadUrlForLayer", body: `{}`},
			{service: "s3", method: "GET", path: "/bucket?session"},
		},
	} {
		for _, c := range calls {
			op := c.classify()
			got := op.level
			if op.refusal != "" {
				got = refusedOp
			}
			assert.Equal(t, want, got, "%+v", c)
		}
	}
}

// A non-REST operation is read only where its row's protocol names it, and a
// request naming it elsewhere or twice is refused.
func TestAnOperationIsReadOnlyWhereItsRowNamesIt(t *testing.T) {
	for _, c := range []call{
		{service: "monitoring", form: "Action=ListMetrics"},
		{service: "monitoring", target: "GraniteServiceVersion20100801.ListMetrics", body: `{}`},
	} {
		op := c.classify()
		assert.Empty(t, op.refusal, "%+v", c)
		assert.Equal(t, "ListMetrics", op.name, "%+v", c)
	}
	for _, c := range []call{
		{service: "ec2", form: "Action=TerminateInstances", target: "AmazonEC2.DescribeInstances"},
		{service: "ec2", target: "AmazonEC2.DescribeInstances", body: `{}`},
		{service: "logs", path: "/?Action=DescribeLogGroups", target: "Logs_20140328.DescribeLogGroups", body: `{}`},
		{service: "logs", path: "/?Action=DescribeLogGroups"},
		{service: "monitoring", form: "Action=PutMetricAlarm", target: "GraniteServiceVersion20100801.ListMetrics"},
		{service: "ec2", form: "Action=DescribeInstances&Action=TerminateInstances"},
		{service: "sts", path: "/?Action=AssumeRole", form: "Action=AssumeRole"},
		{service: "ec2", path: "/?Action=DescribeInstances", form: "Action=TerminateInstances"},
	} {
		assert.Contains(t, c.classify().refusal, "names its operation where", "%+v", c)
	}
}

// A form body is read for its Action however leniently its Content-Type is
// written, as runproxy reads it to show it, so the Action AWS runs is read.
func TestAFormIsReadWhateverItsTypesParameters(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/?Action=DescribeInstances", nil)
	r.Header.Set("Content-Type", "Application/X-WWW-Form-Urlencoded; a=1; a=2")
	op := classify("ec2", r, []byte("Action=TerminateInstances"))
	assert.Contains(t, op.refusal, "names its operation where")
}

// A query or JSON operation is named for the summary; a REST one is not.
func TestTheOperationIsNamed(t *testing.T) {
	assert.Equal(t, "DescribeInstances", call{service: "ec2", form: "Action=DescribeInstances"}.classify().name)
	assert.Equal(t, "GetMetricData", call{service: "monitoring", target: "GraniteServiceVersion20100801.GetMetricData", body: "{}"}.classify().name)
	assert.Empty(t, call{service: "s3", method: "GET", path: "/b"}.classify().name)
}

// An S3 request's bucket is its path's first segment.
func TestAnS3RequestNamesItsBucket(t *testing.T) {
	for path, want := range map[string]string{"/": "", "/b": "b", "/b/": "b", "/b/k/x": "b", "/b?acl": "b"} {
		assert.Equal(t, want, call{service: "s3", method: "GET", path: path}.classify().bucket, path)
	}
	assert.Empty(t, call{service: "ec2", form: "Action=DescribeInstances"}.classify().bucket)
}

// Each refusal of an operation whose answer is a credential is reach, which
// the boundary never allows; a refusal for anything else has no level.
func TestEachCredentialRefusalIsReach(t *testing.T) {
	for _, c := range []call{
		{service: "sts", form: "Action=AssumeRole"},
		{service: "sts", form: "Action=GetSessionToken"},
		{service: "ec2", form: "Action=CreateKeyPair"},
		{service: "ec2", form: "Action=GetPasswordData"},
		{service: "iam", form: "Action=CreateAccessKey"},
		{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken", body: `{}`},
		{service: "s3", method: "GET", path: "/bucket?session"},
	} {
		op := c.classify()
		assert.Contains(t, op.refusal, "answers a credential", "%+v", c)
		assert.Equal(t, permissions.Reach, op.level, "%+v", c)
		act := permissions.Action{Principal: permissions.Chat, Target: permissions.AWS, Scope: "123456789012/eu-west-1", AccessLevel: op.level}
		allow := permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.Reach, Principal: "*", Target: "aws",
			Scope: "123456789012/**", Service: "*", Operation: "*", Resource: "*"}
		v, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{allow}}.Authorize(act)
		assert.Equal(t, permissions.Deny, v.Effect, "%+v", c)
	}
	op := call{service: "ec2", form: "Action=DescribeInstances&Action=TerminateInstances"}.classify()
	assert.NotEmpty(t, op.refusal)
	assert.Empty(t, op.level, "a request named twice has no level")
}

// The changes a mode or a rule may allow.
func TestOrdinaryIsExactlyTheTable(t *testing.T) {
	assert.Equal(t, map[string][]string{
		"ec2":                  {"StartInstances", "CreateTags", "CreateVolume", "CreateSnapshot"},
		"elasticloadbalancing": {"RegisterTargets", "AddTags"},
		"ecr":                  {"InitiateLayerUpload", "UploadLayerPart", "CompleteLayerUpload", "PutImage"},
		"logs":                 {"CreateLogGroup", "CreateLogStream", "PutLogEvents"},
	}, ordinary)
	for service, ops := range ordinary {
		for _, name := range ops {
			c := call{service: service, form: "Action=" + name}
			if services[service].protocol == protocolJSON {
				c = call{service: service, target: "X." + name, body: `{}`}
			}
			op := c.classify()
			assert.Equal(t, permissions.Write, op.level, "%s:%s", service, name)
			assert.False(t, op.once, "%s:%s", service, name)
		}
	}
	// S3's one row: a PUT of an object, with no query but x-id and no ACL,
	// grant or copy source.
	for _, path := range []string{"/bucket/key", "/bucket/a/b?x-id=PutObject"} {
		op := call{service: "s3", method: "PUT", path: path}.classify()
		assert.Equal(t, permissions.Write, op.level, path)
		assert.False(t, op.once, path)
	}
}

// A change off the table asks each time, under every mode and rule.
func TestAChangeOffTheOrdinaryTableAsksOnce(t *testing.T) {
	for _, c := range []call{
		{service: "s3", method: "PUT", path: "/bucket"},
		{service: "s3", method: "PUT", path: "/bucket/key?tagging"},
		{service: "s3", method: "POST", path: "/bucket/key?restore"},
		{service: "eks", method: "POST", path: "/clusters/c/access-entries"},
		{service: "eks", method: "POST", path: "/clusters/c/node-groups/n/update-config"},
		{service: "cloudformation", form: "Action=CreateStack&StackName=s"},
		{service: "ec2", form: "Action=StopInstances&InstanceId.1=i-1"},
		{service: "ec2", form: "Action=RunInstances"},
		{service: "autoscaling", form: "Action=SetDesiredCapacity&DesiredCapacity=0"},
		{service: "autoscaling", form: "Action=UpdateAutoScalingGroup&MaxSize=0"},
		{service: "elasticloadbalancing", form: "Action=DeregisterTargets"},
		{service: "route53", method: "POST", path: "/2013-04-01/hostedzone/Z/rrset"},
		{service: "monitoring", form: "Action=PutMetricAlarm"},
	} {
		op := c.classify()
		assert.Equal(t, permissions.Write, op.level, "%+v", c)
		assert.True(t, op.once, "%+v", c)
	}
}

func TestAnS3PutWithACopySourceAsksOnce(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/bucket/key", nil)
	r.Header.Set("X-Amz-Copy-Source", "other/key")
	op := classify("s3", r, nil)
	assert.Equal(t, permissions.Write, op.level)
	assert.True(t, op.once)
}

// A change to who may reach a bucket or a key is an access control change,
// by subresource or by header, a delete included, so an access control rule
// decides it. A read of one stays a read.
func TestAnS3PolicyOrACLChangeIsAccessControl(t *testing.T) {
	for _, c := range []call{
		{service: "s3", method: "PUT", path: "/bucket?policy"},
		{service: "s3", method: "DELETE", path: "/bucket?policy"},
		{service: "s3", method: "PUT", path: "/bucket?acl"},
		{service: "s3", method: "PUT", path: "/bucket/key?acl"},
		{service: "s3", method: "PUT", path: "/bucket?publicAccessBlock"},
		{service: "s3", method: "DELETE", path: "/bucket?publicAccessBlock"},
		{service: "s3", method: "PUT", path: "/bucket?ownershipControls"},
	} {
		assert.Equal(t, permissions.AccessControl, c.classify().level, "%+v", c)
	}
	for _, header := range []string{"X-Amz-Acl", "X-Amz-Grant-Read", "X-Amz-Grant-Full-Control"} {
		r := httptest.NewRequest(http.MethodPut, "/bucket/key", nil)
		r.Header.Set(header, "x")
		assert.Equal(t, permissions.AccessControl, classify("s3", r, nil).level, header)
	}
	for _, c := range []call{
		{service: "s3", method: "GET", path: "/bucket?policy"},
		{service: "s3", method: "GET", path: "/bucket/key?acl"},
	} {
		assert.Equal(t, permissions.Read, c.classify().level, "%+v", c)
	}
}

func TestAccessControlChanges(t *testing.T) {
	for _, c := range []call{
		{service: "iam", form: "Action=CreateUser"},
		{service: "iam", form: "Action=AttachRolePolicy"},
		{service: "iam", form: "Action=DeleteRolePolicy"},
		{service: "cloudformation", form: "Action=SetStackPolicy"},
		// A registry named in the body, the login's own or another's, leaves the
		// level as it is: being foreign asks each time on top of it.
		{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.SetRepositoryPolicy", body: `{"registryId":"123456789012"}`},
		{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.SetRepositoryPolicy", body: `{"registryId":"210987654321"}`},
	} {
		assert.Equal(t, permissions.AccessControl, c.classify().level, "%+v", c)
	}
	read := call{service: "ecr", target: "AmazonEC2ContainerRegistry_V20150921.GetRepositoryPolicy", body: `{"registryId":"210987654321"}`}.classify()
	assert.Equal(t, permissions.Write, read.level, "a read naming a registry is a write, which another account's log records")
	assert.True(t, read.once)
	// The words are read in the operation's name alone.
	op := call{service: "ec2", form: "Action=ModifyImageAttribute&LaunchPermission.Add.1.UserId=1"}.classify()
	assert.Equal(t, permissions.Write, op.level)
	for _, c := range []call{
		{service: "iam", form: "Action=GetRolePolicy"},
		{service: "iam", form: "Action=ListPolicies"},
		{service: "iam", form: "Action=SimulatePrincipalPolicy"},
	} {
		assert.NotEqual(t, permissions.AccessControl, c.classify().level, "%+v", c)
	}
}

// Each field a request reaches the decision with: service, operation (a
// REST row's method), resource and level.
func TestEveryFieldIsFilled(t *testing.T) {
	for _, c := range []struct {
		call               call
		op, resource, name string
		level              permissions.AccessLevel
	}{
		{call{service: "ec2", form: "Action=StartInstances"}, "StartInstances", "", "", permissions.Write},
		{call{service: "logs", target: "Logs_20140328.PutLogEvents", body: `{}`}, "PutLogEvents", "", "", permissions.Write},
		{call{service: "s3", method: "PUT", path: "/bucket/a/b"}, "PUT", "object", "bucket/a/b", permissions.Write},
		{call{service: "s3", method: "GET", path: "/bucket/a/b"}, "GET", "object", "bucket/a/b", permissions.DataRead},
		{call{service: "s3", method: "PUT", path: "/bucket?policy"}, "PUT", "bucket", "bucket", permissions.AccessControl},
		{call{service: "s3", method: "DELETE", path: "/bucket"}, "DELETE", "bucket", "bucket", permissions.Destructive},
		{call{service: "eks", method: "DELETE", path: "/clusters/c"}, "DELETE", "", "", permissions.Destructive},
		{call{service: "iam", form: "Action=CreateRole"}, "CreateRole", "", "", permissions.AccessControl},
	} {
		op := c.call.classify()
		assert.Equal(t, c.op, op.operation, "%+v", c.call)
		assert.Equal(t, c.resource, op.resource, "%+v", c.call)
		assert.Equal(t, c.name, op.object, "%+v", c.call)
		assert.Equal(t, c.level, op.level, "%+v", c.call)
	}
}
