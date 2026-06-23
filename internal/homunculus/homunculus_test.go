package homunculus

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStripCredentials(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar.git":          "https://github.com/foo/bar.git",
		"https://ghp_XYZ@github.com/foo/bar.git":  "https://github.com/foo/bar.git",
		"https://user:tok@gitlab.com/foo/bar":     "https://gitlab.com/foo/bar",
		"git@github.com:foo/bar.git":              "git@github.com:foo/bar.git",
		"":                                        "",
	}
	for in, want := range cases {
		if got := stripCredentials(in); got != want {
			t.Errorf("stripCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectIDFromHash_Deterministic(t *testing.T) {
	id := projectIDFromHash("https://github.com/foo/bar.git")
	if len(id) != ProjectIDLen {
		t.Errorf("length = %d, want %d", len(id), ProjectIDLen)
	}
	if id != projectIDFromHash("https://github.com/foo/bar.git") {
		t.Errorf("non-deterministic project_id")
	}
	if id == projectIDFromHash("https://github.com/other/repo.git") {
		t.Errorf("different inputs should not collide trivially")
	}
}

func TestProjectNameFromRemote(t *testing.T) {
	cases := []struct {
		remote, root, want string
	}{
		{"https://github.com/foo/bar.git", "/x", "bar"},
		{"git@github.com:foo/bar.git", "/x", "bar"},
		{"", "/path/to/repo", "repo"},
	}
	for _, tc := range cases {
		if got := projectNameFromRemoteOrRoot(tc.remote, tc.root); got != tc.want {
			t.Errorf("projectNameFromRemoteOrRoot(%q,%q) = %q, want %q", tc.remote, tc.root, got, tc.want)
		}
	}
}

func TestLayout_DirsAreUnderRoot(t *testing.T) {
	l := FromRoot("/tmp/bough-test")
	cases := []string{
		l.ProjectsJSON(),
		l.ProjectDir("abc123"),
		l.InstinctsDir("abc123"),
		l.ObservationsFile("abc123"),
		l.ClusterLabels("abc123"),
		l.EvolvedSkillsDir("abc123"),
		l.EvolvedAgentsDir("abc123"),
		l.EvolvedCommandsDir("abc123"),
		l.GlobalInstinctsDir(),
	}
	for _, p := range cases {
		if !strings.HasPrefix(p, l.Root) {
			t.Errorf("%q is not under root %q", p, l.Root)
		}
	}
}

func TestLayout_EnsureProjectDirs(t *testing.T) {
	root := t.TempDir()
	l := FromRoot(root)
	if err := l.EnsureProjectDirs("abc123"); err != nil {
		t.Fatalf("EnsureProjectDirs: %v", err)
	}
	for _, d := range []string{
		l.InstinctsDir("abc123"),
		l.EvolvedSkillsDir("abc123"),
		l.EvolvedAgentsDir("abc123"),
		l.EvolvedCommandsDir("abc123"),
		l.EvalDir("abc123"),
	} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("expected %s to exist: %v", d, err)
		}
	}
}

func TestRegistry_RoundTrip(t *testing.T) {
	root := t.TempDir()
	l := FromRoot(root)
	reg := NewRegistryRW(l)
	reg.SetClock(func() time.Time { return time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC) })

	if err := reg.WriteUpsert(Project{ID: "abc123", Name: "demo", Root: "/x"}); err != nil {
		t.Fatalf("WriteUpsert: %v", err)
	}
	rows, err := reg.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rows["abc123"].Name != "demo" {
		t.Errorf("name = %q, want demo", rows["abc123"].Name)
	}
	if rows["abc123"].CreatedAt.IsZero() {
		t.Errorf("CreatedAt was not stamped")
	}
}

func TestRegistry_UpsertPreservesCreatedAt(t *testing.T) {
	root := t.TempDir()
	l := FromRoot(root)
	reg := NewRegistryRW(l)
	t0 := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	reg.SetClock(func() time.Time { return t0 })
	_ = reg.WriteUpsert(Project{ID: "x", Name: "demo", Root: "/x"})

	reg.SetClock(func() time.Time { return t1 })
	_ = reg.WriteUpsert(Project{ID: "x", Name: "demo-renamed", Root: "/x"})

	rows, _ := reg.Read()
	if !rows["x"].CreatedAt.Equal(t0) {
		t.Errorf("CreatedAt = %s, want %s (preserve on upsert)", rows["x"].CreatedAt, t0)
	}
	if !rows["x"].LastSeen.Equal(t1) {
		t.Errorf("LastSeen = %s, want %s", rows["x"].LastSeen, t1)
	}
	if rows["x"].Name != "demo-renamed" {
		t.Errorf("name = %q, want demo-renamed", rows["x"].Name)
	}
}

const sampleInstinct = `---
id: read-before-edit
trigger: when editing unfamiliar files
confidence: 0.7
domain: workflow
scope: project
observed: 5
first_seen: 2026-04-15T00:00:00Z
last_seen: 2026-06-20T00:00:00Z
---

## Action
Read the surrounding implementation before editing.

## Evidence
- Observed 5 times.
`

func TestReadInstinctFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read-before-edit.md")
	if err := os.WriteFile(path, []byte(sampleInstinct), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	in, err := ReadInstinctFile(path)
	if err != nil {
		t.Fatalf("ReadInstinctFile: %v", err)
	}
	if in.ID != "read-before-edit" {
		t.Errorf("ID = %q", in.ID)
	}
	if in.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", in.Confidence)
	}
	if in.Domain != "workflow" {
		t.Errorf("Domain = %q, want workflow", in.Domain)
	}
	if !strings.Contains(in.Body, "Read the surrounding implementation") {
		t.Errorf("body missing expected text: %q", in.Body)
	}
}

func TestReadInstinctFile_IDMismatch(t *testing.T) {
	dir := t.TempDir()
	// filename says "x", frontmatter says "read-before-edit"
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte(sampleInstinct), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ReadInstinctFile(path)
	if !errors.Is(err, ErrIDMismatch) {
		t.Errorf("err = %v, want ErrIDMismatch", err)
	}
}

func TestWriteInstinctFile_Atomic(t *testing.T) {
	dir := t.TempDir()
	in := &Instinct{
		ID:         "atomic-write",
		Trigger:    "when testing",
		Confidence: 0.7,
		Domain:     "testing",
		Scope:      "project",
		Body:       "## Action\nDo the thing.",
	}
	path, err := WriteInstinctFile(dir, in)
	if err != nil {
		t.Fatalf("WriteInstinctFile: %v", err)
	}
	if filepath.Base(path) != "atomic-write.md" {
		t.Errorf("filename = %q, want atomic-write.md", filepath.Base(path))
	}
	// No .tmp leftover.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("leftover .tmp: %v", err)
	}
	// Round-trip read.
	got, err := ReadInstinctFile(path)
	if err != nil {
		t.Fatalf("read-back: %v", err)
	}
	if got.ID != "atomic-write" || got.Domain != "testing" {
		t.Errorf("round-trip lost fields: %+v", got)
	}
}

func TestWriteInstinctFile_RejectsBadID(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", "BadID", "with space", "trailing-", "double--dash"} {
		if _, err := WriteInstinctFile(dir, &Instinct{ID: bad}); !errors.Is(err, ErrIDInvalid) {
			t.Errorf("WriteInstinctFile(%q) err = %v, want ErrIDInvalid", bad, err)
		}
	}
}

func TestScanInstincts_SkipsCatalogFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "INSTINCTS.md"), []byte("# index"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# memory"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "read-before-edit.md"), []byte(sampleInstinct), 0o644)
	rows, errs := ScanInstincts(dir)
	if len(rows) != 1 {
		t.Errorf("rows = %d, want 1 (only the real instinct)", len(rows))
	}
	if len(errs) != 0 {
		t.Errorf("unexpected soft errors: %v", errs)
	}
}
