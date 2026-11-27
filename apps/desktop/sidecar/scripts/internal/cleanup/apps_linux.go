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

package cleanup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FindApps is the installed .deb or .rpm, removed through its package
// manager, and a note naming each AppImage found, which is the user's to
// delete.
func FindApps(home string) ([]App, []string, error) {
	var apps []App
	if manager := installedBy(); manager != "" {
		cmd := linuxRemoveCommand(manager, os.Geteuid() == 0)
		apps = append(apps, App{
			Name:   fmt.Sprintf("the %s package (%s)", linuxPackage, strings.Join(cmd, " ")),
			remove: func() error { return run(cmd[0], cmd[1:]...) },
		})
	}
	var notes []string
	for _, path := range appImages(home) {
		notes = append(notes, fmt.Sprintf("an AppImage is a file of its own and is left alone: delete %s yourself", path))
	}
	return apps, notes, nil
}

// installedBy is the package manager that installed the package, or empty.
func installedBy() string {
	out, err := exec.Command("dpkg-query", "-W", "-f=${db:Status-Status}", linuxPackage).Output()
	if err == nil && strings.TrimSpace(string(out)) == "installed" {
		return "apt-get"
	}
	if exec.Command("rpm", "-q", linuxPackage).Run() == nil {
		return "dnf"
	}
	return ""
}

// run runs name with args on the user's terminal.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
