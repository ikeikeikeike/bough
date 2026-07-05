//go:build darwin || linux

package composeutil

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

const fixtureCompose = `services:
  db:
    image: mysql:8.4
    volumes:
      - dbstore:/var/lib/mysql
    ports:
      - "3306:3306"
    environment:
      - MYSQL_ALLOW_EMPTY_PASSWORD=1
    healthcheck:
      test: ["CMD", "mysqladmin", "ping"]
      interval: 10s
  redis:
    image: redis:6.2
    ports:
      - "6379:6379"
volumes:
  dbstore:
`

func writeFixture(t *testing.T) (dir, composeFile string) {
	t.Helper()
	dir = t.TempDir()
	composeFile = filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(composeFile, []byte(fixtureCompose), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir, composeFile
}

func renderAndParse(t *testing.T, service, target string, hostPort int) map[string]any {
	t.Helper()
	dir, composeFile := writeFixture(t)
	dst := filepath.Join(dir, "derived.yml")
	if err := Render(composeFile, service, target, hostPort, dst); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read derived: %v", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse derived: %v", err)
	}
	return doc
}

func svc(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	services, ok := doc["services"].(map[string]any)
	if !ok {
		t.Fatalf("derived has no services mapping")
	}
	s, ok := services[name].(map[string]any)
	if !ok {
		t.Fatalf("derived has no service %q", name)
	}
	return s
}

// TestRender_RewritesTargetServicePortToBoughHostPort is the core guard:
// the one service's published port becomes a single 127.0.0.1:<host>:<target>
// mapping (not concatenated with the original, which is the compose
// override-merge trap this whole approach exists to sidestep).
func TestRender_RewritesTargetServicePortToBoughHostPort(t *testing.T) {
	doc := renderAndParse(t, "db", "3306", 42931)
	ports, ok := svc(t, doc, "db")["ports"].([]any)
	if !ok || len(ports) != 1 {
		t.Fatalf("db ports = %v, want exactly one entry", svc(t, doc, "db")["ports"])
	}
	if got, want := ports[0], "127.0.0.1:42931:3306"; got != want {
		t.Errorf("db ports[0] = %q, want %q", got, want)
	}
}

// TestRender_AdoptsEverythingElseVerbatim proves bough only touches the
// port: image, volume mount, and healthcheck of the target service — and
// the untouched sibling service — survive the round-trip unchanged.
func TestRender_AdoptsEverythingElseVerbatim(t *testing.T) {
	doc := renderAndParse(t, "db", "3306", 42931)
	db := svc(t, doc, "db")
	if db["image"] != "mysql:8.4" {
		t.Errorf("db image = %v, want mysql:8.4 (must be adopted verbatim)", db["image"])
	}
	if _, ok := db["healthcheck"]; !ok {
		t.Error("db healthcheck dropped — the compose file must stay the source of truth")
	}
	if vols, ok := db["volumes"].([]any); !ok || len(vols) != 1 || vols[0] != "dbstore:/var/lib/mysql" {
		t.Errorf("db volumes = %v, want [dbstore:/var/lib/mysql]", db["volumes"])
	}
	// Sibling service left completely alone (a different engine owns it).
	redis := svc(t, doc, "redis")
	if rp, ok := redis["ports"].([]any); !ok || len(rp) != 1 || rp[0] != "6379:6379" {
		t.Errorf("redis ports = %v, want the original [6379:6379] untouched", redis["ports"])
	}
}

func TestRender_ErrorsOnMissingService(t *testing.T) {
	_, composeFile := writeFixture(t)
	dst := filepath.Join(filepath.Dir(composeFile), "derived.yml")
	if err := Render(composeFile, "nonexistent", "3306", 42931, dst); err == nil {
		t.Fatal("Render on a missing service should error, got nil")
	}
}

func TestProject_Format(t *testing.T) {
	if got, want := Project("mysql", 42931), "bough-mysql-42931"; got != want {
		t.Errorf("Project = %q, want %q", got, want)
	}
}
