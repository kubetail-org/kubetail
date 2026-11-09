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

// Package gomodules maps the file names a Go cover profile holds, which are
// import paths, to files on disk.
package gomodules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Modules maps each module's path to its directory.
type Modules map[string]string

// Load reads the modules in dir: every module its go.work uses, or else the
// one its go.mod declares.
func Load(dir string) (Modules, error) {
	gowork := filepath.Join(dir, "go.work")
	data, err := os.ReadFile(gowork)
	if os.IsNotExist(err) {
		return loadModule(dir)
	}
	if err != nil {
		return nil, err
	}
	work, err := modfile.ParseWork(gowork, data, nil)
	if err != nil {
		return nil, err
	}
	mods := Modules{}
	for _, use := range work.Use {
		m, err := loadModule(filepath.Join(dir, filepath.FromSlash(use.Path)))
		if err != nil {
			return nil, err
		}
		for path, dir := range m {
			mods[path] = dir
		}
	}
	return mods, nil
}

func loadModule(dir string) (Modules, error) {
	gomod := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil, err
	}
	path := modfile.ModulePath(data)
	if path == "" {
		return nil, fmt.Errorf("%s: no module line", gomod)
	}
	return Modules{path: dir}, nil
}

// File returns the file on disk that a profile's file name names. The longest
// module path wins, so a module nested in another maps its own files.
func (m Modules) File(name string) (string, error) {
	best := ""
	for path := range m {
		if strings.HasPrefix(name, path+"/") && len(path) > len(best) {
			best = path
		}
	}
	if best == "" {
		return "", fmt.Errorf("%s is outside every module", name)
	}
	rel := strings.TrimPrefix(name, best+"/")
	return filepath.Join(m[best], filepath.FromSlash(rel)), nil
}
