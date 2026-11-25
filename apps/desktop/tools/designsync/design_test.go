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
