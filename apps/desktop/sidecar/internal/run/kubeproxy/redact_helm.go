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

package kubeproxy

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	yamlv2 "go.yaml.in/yaml/v2"
	"sigs.k8s.io/yaml"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
)

// helmReleaseType is the type of the Secret helm keeps a release in.
const helmReleaseType = "helm.sh/release.v1"

// gzipMagic leads a release helm gzipped.
var gzipMagic = []byte{0x1f, 0x8b, 0x08}

// redactRelease redacts a release Secret's data.release, value, inside: the
// values the user supplied and the Secrets in its manifests. The release is
// helm's encoding under the Secret's base64: base64, then gzip, then JSON.
func redactRelease(value string) (string, error) {
	release, err := decodeRelease(value)
	if err != nil {
		return "", err
	}
	if err := redactConfig(release); err != nil {
		return "", err
	}
	if err := redactManifests(release); err != nil {
		return "", err
	}
	return encodeRelease(release), nil
}

// decodeRelease is value's release, its top level alone parsed.
func decodeRelease(value string) (map[string]json.RawMessage, error) {
	helm, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%w: a release that is not base64", errUnredactable)
	}
	body, err := base64.StdEncoding.DecodeString(string(helm))
	if err != nil {
		return nil, fmt.Errorf("%w: a release that is not base64", errUnredactable)
	}
	if bytes.HasPrefix(body, gzipMagic) {
		if body, err = gunzip(body, maxRelease); err != nil {
			return nil, err
		}
	}
	var release map[string]json.RawMessage
	if err := json.Unmarshal(body, &release); err != nil || release == nil {
		return nil, fmt.Errorf("%w: a release that is not a JSON object", errUnredactable)
	}
	return release, nil
}

// gunzip inflates body, refusing one past limit bytes.
func gunzip(body []byte, limit int) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: a release that is not gzip", errUnredactable)
	}
	out, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: a release that is not gzip", errUnredactable)
	}
	if len(out) > limit {
		return nil, fmt.Errorf("%w: a release past %d bytes", errUnredactable, limit)
	}
	return out, nil
}

// maxRelease is the most a release inflates to, so a small value cannot
// expand without bound.
const maxRelease = 64 << 20

// redactConfig replaces every scalar of the release's config, the values the
// user supplied, keeping its shape.
func redactConfig(release map[string]json.RawMessage) error {
	raw, ok := release["config"]
	if !ok {
		return nil
	}
	var config any
	// raw came out of json.Unmarshal, so it is JSON.
	_ = json.Unmarshal(raw, &config)
	if config == nil {
		return nil
	}
	if _, ok := config.(map[string]any); !ok {
		return fmt.Errorf("%w: a release config that is not an object", errUnredactable)
	}
	release["config"] = marshal(scalarsRedacted(config))
	return nil
}

// scalarsRedacted is v with every scalar at any depth the mark.
func scalarsRedacted(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, e := range v {
			v[k] = scalarsRedacted(e)
		}
		return v
	case []any:
		for i, e := range v {
			v[i] = scalarsRedacted(e)
		}
		return v
	}
	return safe.Redacted
}

// redactManifests redacts the Secrets in the release's manifest and in each
// hook's.
func redactManifests(release map[string]json.RawMessage) error {
	if err := redactManifestField(release); err != nil {
		return err
	}
	raw, ok := release["hooks"]
	if !ok {
		return nil
	}
	var hooks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &hooks); err != nil {
		return fmt.Errorf("%w: release hooks that are not a list of objects", errUnredactable)
	}
	for _, hook := range hooks {
		if hook == nil {
			return fmt.Errorf("%w: a release hook that is not an object", errUnredactable)
		}
		if err := redactManifestField(hook); err != nil {
			return err
		}
	}
	release["hooks"] = marshal(hooks)
	return nil
}

// redactManifestField redacts the Secrets in obj's manifest, a string or null.
func redactManifestField(obj map[string]json.RawMessage) error {
	raw, ok := obj["manifest"]
	if !ok {
		return nil
	}
	var manifest *string
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("%w: a release manifest that is not a string", errUnredactable)
	}
	if manifest == nil {
		return nil
	}
	redacted, err := redactManifest(*manifest)
	if err != nil {
		return err
	}
	obj["manifest"] = marshal(redacted)
	return nil
}

// manifestSeparator is the separator helm splits a manifest on
// (releaseutil.SplitManifests), so every document helm or kubectl finds is
// one here.
var manifestSeparator = regexp.MustCompile(`(?:^|\s*\n)---\s*`)

// redactManifest is manifest with the values of every Secret in it redacted.
// The separators and every document holding no Secret keep their bytes.
func redactManifest(manifest string) (string, error) {
	var out strings.Builder
	start := 0
	emit := func(end int) error {
		// helm's separator can end on the line a document starts on.
		midLine := start > 0 && manifest[start-1] != '\n'
		doc, err := redactDocument(manifest[start:end], midLine)
		out.WriteString(doc)
		return err
	}
	for _, sep := range manifestSeparator.FindAllStringIndex(manifest, -1) {
		if err := emit(sep[0]); err != nil {
			return "", err
		}
		out.WriteString(manifest[sep[0]:sep[1]])
		start = sep[1]
	}
	if err := emit(len(manifest)); err != nil {
		return "", err
	}
	return out.String(), nil
}

// redactDocument is doc with the values of every Secret in it redacted. A
// document holding one is encoded again, keeping only helm's # Source: line
// of its comments, since a template can render a value into one. midLine is
// whether doc starts on its separator's line.
func redactDocument(doc string, midLine bool) (string, error) {
	parsed, err := oneDocument(doc)
	if err != nil || !holdsSecret(parsed) {
		return doc, err
	}
	var obj any
	if err := yaml.Unmarshal([]byte(doc), &obj, useNumber); err != nil {
		return "", fmt.Errorf("%w: a manifest document that is not YAML", errUnredactable)
	}
	if err := redactSecrets(obj); err != nil {
		return "", err
	}
	// obj came out of yaml.Unmarshal, so it encodes.
	body, _ := yaml.Marshal(obj)
	var out strings.Builder
	if midLine {
		out.WriteString("\n")
	}
	for line := range strings.SplitSeq(doc, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		if strings.HasPrefix(line, "# Source:") {
			out.WriteString(line + "\n")
		}
	}
	out.WriteString(strings.TrimSuffix(string(body), "\n"))
	out.WriteString(doc[len(strings.TrimRight(doc, " \t\r\n")):])
	return out.String(), nil
}

// oneDocument is doc decoded, refused unless it holds at most one YAML
// document: helm's split finds every ---, but a ... ends a document and
// another may follow, and yaml.Unmarshal reads only the first.
func oneDocument(doc string) (any, error) {
	dec := yamlv2.NewDecoder(strings.NewReader(doc))
	var v, next any
	if err := dec.Decode(&v); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: a manifest document that is not YAML", errUnredactable)
	}
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: a manifest document holding another", errUnredactable)
	}
	return v, nil
}

// holdsSecret is whether v, as go-yaml decodes a document, holds a map whose
// kind is Secret or SecretList at any depth, so a Secret in a List is one too.
func holdsSecret(v any) bool {
	switch v := v.(type) {
	case map[any]any:
		if v["kind"] == "Secret" || v["kind"] == "SecretList" {
			return true
		}
		for _, e := range v {
			if holdsSecret(e) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(v, holdsSecret) {
			return true
		}
	}
	return false
}

func useNumber(d *json.Decoder) *json.Decoder {
	d.UseNumber()
	return d
}

// redactSecrets redacts, in place, the values of every Secret in v at any
// depth, and every last-applied-configuration, which holds a Secret whole.
// Every item of a SecretList is a Secret, whatever kind it names, since
// Kubernetes takes it from the list.
func redactSecrets(v any) error {
	return walk(v, func(m map[string]any) error {
		if _, ok := m[lastApplied]; ok {
			m[lastApplied] = safe.Redacted
		}
		switch m["kind"] {
		case "Secret":
			return redactValues(m)
		case "SecretList":
			items, _ := m["items"].([]any)
			for _, item := range items {
				if item, ok := item.(map[string]any); ok {
					if err := redactValues(item); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

// encodeRelease is release encoded as helm reads it, under the Secret's
// base64.
func encodeRelease(release map[string]json.RawMessage) string {
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	// A write to a bytes.Buffer never fails.
	_, _ = zw.Write(marshal(release))
	_ = zw.Close()
	helm := base64.StdEncoding.EncodeToString(zipped.Bytes())
	return base64.StdEncoding.EncodeToString([]byte(helm))
}

// marshal is v as JSON with no HTML escaped, so what passes keeps its bytes.
// v is always JSON decoded, or built from it, so it always encodes.
func marshal(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}
