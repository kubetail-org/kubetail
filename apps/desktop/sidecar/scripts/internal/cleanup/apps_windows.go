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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// uninstallKey lists every installed program, under each hive and view.
const uninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

// FindApps is each installer the registry lists for Kubetail: the MSI, per
// machine, or the NSIS installer of a local build, per user. Each is run
// rather than its folder deleted, so it removes its shortcuts and its entry.
func FindApps(string) ([]App, []string, error) {
	var apps []App
	var notes []string
	seen := map[string]bool{}
	hives := []struct {
		name string
		key  registry.Key
	}{{"HKCU", registry.CURRENT_USER}, {"HKLM", registry.LOCAL_MACHINE}}
	for _, hive := range hives {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			found, err := uninstallEntries(hive.key, view)
			if err != nil {
				return nil, nil, err
			}
			for _, e := range found {
				id := hive.name + `\` + e.key
				if seen[id] {
					continue
				}
				seen[id] = true
				program, cmdLine, err := windowsUninstallCommand(e.key, e.msi, e.uninstallString)
				if err != nil {
					return nil, nil, err
				}
				if !e.msi && len(notes) == 0 {
					notes = append(notes, "an NSIS uninstaller returns before it finishes: check it has before testing an install")
				}
				apps = append(apps, App{
					Name:   fmt.Sprintf("%s's installer entry %s (%s)", hive.name, e.key, cmdLine),
					remove: func() error { return runCmdLine(program, cmdLine) },
				})
			}
		}
	}
	return apps, notes, nil
}

// entry is one subkey of the Uninstall key that names Kubetail.
type entry struct {
	key             string
	msi             bool
	uninstallString string
}

func uninstallEntries(hive registry.Key, view uint32) ([]entry, error) {
	k, err := registry.OpenKey(hive, uninstallKey, registry.ENUMERATE_SUB_KEYS|view)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil, err
	}
	var found []entry
	for _, name := range names {
		sub, err := registry.OpenKey(k, name, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		display, _, _ := sub.GetStringValue("DisplayName")
		if display == productName {
			msi, _, _ := sub.GetIntegerValue("WindowsInstaller")
			uninstall, _, _ := sub.GetStringValue("UninstallString")
			found = append(found, entry{key: name, msi: msi == 1, uninstallString: uninstall})
		}
		sub.Close()
	}
	return found, nil
}

// runCmdLine runs program with cmdLine passed as is: an UninstallString is a
// command line, quoted for the program, not for Go's argument escaping.
func runCmdLine(program, cmdLine string) error {
	path, err := exec.LookPath(program)
	if err != nil {
		return err
	}
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
