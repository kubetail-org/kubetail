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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	figmaAPI    = "https://api.figma.com/v1"
	desktopFile = "UTfwwz8cyLq5RA6mNFxUsD" // design/CLAUDE.md's Kubetail Desktop
)

// A figmaFile reads one Figma file over the REST API, with a personal access
// token that can read file content.
type figmaFile struct {
	api    string // the API's base URL
	key    string
	token  string
	client *http.Client
}

// read returns the file's components and screens with their hashes and stamps.
func (f figmaFile) read() ([]stamped, error) {
	var file struct {
		Document struct {
			Children []struct{ ID, Name string }
		}
	}
	if err := f.get("/files/"+f.key, url.Values{"depth": {"1"}}, &file); err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, p := range file.Document.Children {
		ids[p.Name] = p.ID
	}
	components, screens := ids["Components"], ids["Screens"]
	if components == "" || screens == "" {
		return nil, fmt.Errorf("the Figma file %s has no Components page or no Screens page", f.key)
	}

	var pages struct {
		Nodes map[string]*page
	}
	// Only geometry=paths returns each layer's size, position and path data.
	query := url.Values{"ids": {components + "," + screens}, "geometry": {"paths"}}
	if err := f.get("/files/"+f.key+"/nodes", query, &pages); err != nil {
		return nil, err
	}
	c, s := pages.Nodes[components], pages.Nodes[screens]
	if c == nil || s == nil {
		return nil, fmt.Errorf("the Figma file %s returned no Components page or no Screens page", f.key)
	}
	return readPages(c, s), nil
}

// get decodes the JSON the API returns for path into v.
func (f figmaFile) get(path string, query url.Values, v any) error {
	req, err := http.NewRequest(http.MethodGet, f.api+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Figma-Token", f.token)
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Figma explains a refusal in a short JSON body.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("reading Figma: GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("reading Figma: GET %s: %w", path, err)
	}
	return nil
}
