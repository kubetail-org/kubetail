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
	"reflect"
	"testing"
)

func TestReadPagesListsTheComponentsAndScreens(t *testing.T) {
	var got []stamped
	for _, s := range readPages(fixturePages(t)) {
		s.Hash = ""
		got = append(got, s)
	}
	want := []stamped{
		{Node: "9:148", Name: "StatusPill", Stamp: &stamp{Kubetail: 1, ID: "dashboard/status-pill", Changes: []string{"It's rounder."}}},
		{Node: "5:21", Name: "BellButton", Stamp: &stamp{Kubetail: 1, ID: "shell/bell-button"}},
		{Node: "7:1", Name: "New", Invalid: true},
		// Popups on Shell holds only a section, so it is no screen.
		{Node: "42:1", Name: "Dashboard", Stamp: &stamp{Kubetail: 1, ID: "screens/dashboard"}},
		{Node: "44:1", Name: "Detail"},
		{Node: "43:2", Name: "Menu", Stamp: &stamp{Kubetail: 1, ID: "screens/popups-on-shell/menu"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("readPages =\n%+v\nwant\n%+v", got, want)
	}
}
