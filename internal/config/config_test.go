package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_validExample(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "example.yaml"))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if got, want := c.SchemaVersion, 1; got != want {
		t.Errorf("SchemaVersion: got %d want %d", got, want)
	}
	if got, want := len(c.Repositories), 3; got != want {
		t.Fatalf("Repositories: got %d want %d", got, want)
	}
	var sawProvider bool
	for _, r := range c.Repositories {
		if r.Role == "db-provider" {
			sawProvider = true
		}
	}
	if !sawProvider {
		t.Errorf("expected exactly one repository with role: db-provider")
	}
	if got, want := len(c.Databases), 1; got != want {
		t.Fatalf("Databases: got %d want %d", got, want)
	}
	if got, want := c.Databases[0].Kind, "mysql"; got != want {
		t.Errorf("Database.Kind: got %q want %q", got, want)
	}
	if got, want := c.Databases[0].PortRange, [2]int{42000, 44999}; got != want {
		t.Errorf("Database.PortRange: got %v want %v", got, want)
	}
}

// Each entry exercises one of the validateSemantic / struct-tag failure
// modes. The test asserts both that Load returns an error and that the
// error message contains an identifying substring, so a future drift in
// validator output is caught without forcing exact-string matches.
func TestLoad_rejectsInvalid(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantInErr string
	}{
		{
			name: "missing schema_version",
			yaml: `monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "SchemaVersion",
		},
		{
			name: "zero repositories",
			yaml: `schema_version: 1
monorepo_root: "."
repositories: []
registry: {path: .worktree-ports.json}
`,
			wantInErr: "Repositories",
		},
		{
			name: "database without db-provider repo",
			yaml: `schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop}
databases:
  - {kind: mysql, version: "8.4", port_range: [42000, 44999]}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "db-provider",
		},
		{
			name: "duplicate repository name",
			yaml: `schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop}
  - {name: a, branch_strategy: develop}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "duplicated",
		},
		{
			name: "invalid port range",
			yaml: `schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop, role: db-provider}
databases:
  - {kind: mysql, version: "8.4", port_range: [42000, 42000]}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "port_range",
		},
		{
			name: "invalid role value",
			yaml: `schema_version: 1
monorepo_root: "."
repositories:
  - {name: a, branch_strategy: develop, role: invalid-role}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "Role",
		},
		{
			name: "unknown top-level field (strict mode)",
			yaml: `schema_version: 1
monorepo_root: "."
typo_field: 1
repositories:
  - {name: a, branch_strategy: develop}
registry: {path: .worktree-ports.json}
`,
			wantInErr: "typo_field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpdir := t.TempDir()
			path := filepath.Join(tmpdir, "config.yaml")
			if err := writeFile(t, path, tc.yaml); err != nil {
				t.Fatalf("writeFile: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantInErr)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantInErr)
			}
		})
	}
}
