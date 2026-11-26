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

package catalog

import (
	"slices"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/toolsettings"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/agent"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/anthropicwebsearch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/edit"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/kubequery"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsread"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logsview"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/logswatch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/memory"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/read"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/taskstop"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/webfetch"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/write"
)

// toolListings is every tool on any list, in the order Settings draws them.
// Turning one off only takes it out of a turn's offer: a setting never adds a
// tool to a list. TaskStop is always on, so a turn can stop the background work
// it started.
var toolListings = []toolsettings.Listing{
	{Name: bash.Name, Label: "Bash", Group: toolsettings.GroupCommands, Description: "Run shell commands, in the sandbox where there is one."},
	{Name: taskstop.Name, Label: "TaskStop", Group: toolsettings.GroupCommands, Description: "Stop a background command, agent or log watch.", AlwaysOn: true},
	{Name: read.Name, Label: "Read", Group: toolsettings.GroupFiles, Description: "Read a file."},
	{Name: write.Name, Label: "Write", Group: toolsettings.GroupFiles, Description: "Create or replace a file."},
	{Name: edit.Name, Label: "Edit", Group: toolsettings.GroupFiles, Description: "Change part of a file."},
	{Name: webfetch.Name, Label: "WebFetch", Group: toolsettings.GroupWeb, Description: "Fetch a web page over HTTPS."},
	{Name: anthropicwebsearch.Name, Label: "WebSearch", Group: toolsettings.GroupWeb, Description: "Search the web, through Anthropic's models only."},
	{Name: kubequery.Name, Label: "KubeQuery", Group: toolsettings.GroupCluster, Description: "Query the cluster's cached objects with SQL."},
	{Name: logsview.Name, Label: "LogsView", Group: toolsettings.GroupLogs, Description: "Open a log view for you to look at."},
	{Name: logsread.Name, Label: "LogsRead", Group: toolsettings.GroupLogs, Description: "Read a window of logs."},
	{Name: logswatch.Name, Label: "LogsWatch", Group: toolsettings.GroupLogs, Description: "Watch logs in the background until a line matches."},
	{Name: memory.Name, Label: "Memory", Group: toolsettings.GroupMemory, Description: "Save and edit notes kept across chats."},
	{Name: agent.Name, Label: "Agent", Group: toolsettings.GroupAgents, Description: "Hand a task to a subagent that runs in the background."},
}

// ToolListings is every tool a chat can be offered, in the order Settings draws
// them.
func ToolListings() []toolsettings.Listing { return toolListings }

// AlwaysOn reports whether the tool named name is listed always on, so no
// setting turns it off.
func (c Catalog) AlwaysOn(name string) bool {
	return slices.ContainsFunc(toolListings, func(l toolsettings.Listing) bool { return l.Name == name && l.AlwaysOn })
}
