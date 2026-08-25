// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wconfig

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/wavetermdev/waveterm/pkg/wconfig/defaultconfig"
)

const sampleProjectsJSON = `{
  "projeto1": {
    "label": "Projeto 1",
    "path": "C:/dev/projeto1",
    "repourl": "https://github.com/rSaintH/projeto1",
    "produrl": "https://projeto1.example.com",
    "description": "descricao",
    "display:order": 2,
    "display:hidden": true,
    "icon": "code-branch",
    "color": "#58c7f3"
  }
}`

// Locks in the JSON tag names that projects.json is written against.
func TestProjectConfigJSONTags(t *testing.T) {
	var projects map[string]ProjectConfigType
	if err := json.Unmarshal([]byte(sampleProjectsJSON), &projects); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	proj, ok := projects["projeto1"]
	if !ok {
		t.Fatalf("projeto1 missing, got keys %v", projects)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"label", proj.Label, "Projeto 1"},
		{"path", proj.Path, "C:/dev/projeto1"},
		{"repourl", proj.RepoUrl, "https://github.com/rSaintH/projeto1"},
		{"produrl", proj.ProdUrl, "https://projeto1.example.com"},
		{"description", proj.Description, "descricao"},
		{"display:order", proj.DisplayOrder, float64(2)},
		{"display:hidden", proj.DisplayHidden, true},
		{"icon", proj.Icon, "code-branch"},
		{"color", proj.Color, "#58c7f3"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// A minimal project entry must round-trip to just its path, so hand-written
// projects.json files stay small and omitempty does not drop the path.
func TestProjectConfigMinimalRoundTrip(t *testing.T) {
	out, err := json.Marshal(ProjectConfigType{Path: "C:/dev/projeto1"})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	const want = `{"path":"C:/dev/projeto1"}`
	if string(out) != want {
		t.Errorf("marshal = %s, want %s", out, want)
	}
}

// projects.json must be wired into FullConfigType so the config loader picks the
// file up by its json tag, the same way widgets.json is loaded.
func TestFullConfigHasProjectsField(t *testing.T) {
	var full FullConfigType
	if err := json.Unmarshal([]byte(`{"projects":`+sampleProjectsJSON+`}`), &full); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if full.Projects == nil {
		t.Fatalf("FullConfigType.Projects is nil; json tag \"projects\" is not wired up")
	}
	if got := full.Projects["projeto1"].Path; got != "C:/dev/projeto1" {
		t.Errorf("projects[projeto1].path = %q, want C:/dev/projeto1", got)
	}
}

// The shipped default must be an empty object so a fresh install shows no
// projects rather than failing to parse.
func TestDefaultProjectsConfigIsEmpty(t *testing.T) {
	data, err := fs.ReadFile(defaultconfig.ConfigFS, "projects.json")
	if err != nil {
		t.Fatalf("projects.json missing from embedded defaultconfig: %v", err)
	}
	var projects map[string]ProjectConfigType
	if err := json.Unmarshal(data, &projects); err != nil {
		t.Fatalf("default projects.json does not parse: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("default projects.json should be empty, got %v", projects)
	}
}
