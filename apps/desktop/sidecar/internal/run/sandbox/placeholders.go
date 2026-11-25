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

package sandbox

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The AWS mount's values that are not the run's own: its key id is the prefix
// and the grant's token, and its secret signs nothing AWS accepts.
const (
	AWSKeyIDPrefix       = "KUBETAIL"
	AWSSecretPlaceholder = "kubetail-not-a-secret-the-proxy-signs"
)

var (
	awsRegionRE  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	awsKeyIDRE   = regexp.MustCompile(`^` + regexp.QuoteMeta(AWSKeyIDPrefix) + `[0-9A-F]{64}$`)
	awsProfileRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@+:/-]{0,127}$`)
)

// AWSRegion is whether region is one of AWS's commercial partition, the only
// one a run reaches: not China's (cn-), GovCloud's (us-gov-) or an isolated
// partition's, whose second part starts iso (us-iso-east-1, eu-isoe-west-1).
func AWSRegion(region string) bool {
	if !awsRegionRE.MatchString(region) {
		return false
	}
	parts := strings.Split(region, "-")
	return parts[0] != "cn" && !strings.HasPrefix(region, "us-gov-") && !strings.HasPrefix(parts[1], "iso")
}

// AWSProfile is whether profile is a name the AWS mount may write into the
// run's config file as a section header: no bracket, space, comment mark or
// line break that could end the header or start another.
func AWSProfile(profile string) bool { return awsProfileRE.MatchString(profile) }

// AWSConfigPath is the AWS config file a mount writes in the run directory
// dir, which a client reads in place of one in the workspace.
func AWSConfigPath(dir string) string { return filepath.Join(dir, "aws", "config") }

// placeholders are the names Unpassable matches that a run may still hold,
// each only with a value a cloud platform's mount writes, so a real credential
// fails the run whoever put it in Run.Env.
var placeholders = map[string]func(r Run, value string) bool{
	"AWS_ENDPOINT_URL": func(r Run, value string) bool {
		port, ok := strings.CutPrefix(value, "http://127.0.0.1:")
		return ok && slices.ContainsFunc(r.Policy.Network.Relays, func(relay Relay) bool {
			return strconv.Itoa(relay.Port) == port
		})
	},
	"AWS_ACCESS_KEY_ID":         func(_ Run, value string) bool { return awsKeyIDRE.MatchString(value) },
	"AWS_SECRET_ACCESS_KEY":     func(_ Run, value string) bool { return value == AWSSecretPlaceholder },
	"AWS_REGION":                func(_ Run, value string) bool { return AWSRegion(value) },
	"AWS_DEFAULT_REGION":        func(_ Run, value string) bool { return AWSRegion(value) },
	"AWS_EC2_METADATA_DISABLED": func(_ Run, value string) bool { return value == "true" },
	"AWS_CONFIG_FILE": func(r Run, value string) bool {
		return slices.ContainsFunc(r.Policy.Always.Read, func(dir string) bool {
			return value == AWSConfigPath(dir)
		})
	},
}

// placeholder is whether name=value is a mount's placeholder in r.
func (r Run) placeholder(name, value string) bool {
	ok, known := placeholders[name]
	return known && ok(r, value)
}
