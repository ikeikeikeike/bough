package homunculus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ProjectIDLen is how many hex chars of the sha256 digest we keep as
// the canonical project_id. The ECC reference uses 12 (= same hash
// space, same collision odds in practice). bough mirrors that so an
// operator who later runs `bough ecc import` can match by id.
const ProjectIDLen = 12

// Project mirrors one row in projects.json. The shape matches what
// the ECC reference reads / writes, intentionally — letting an
// operator point bough at an existing ecc-homunculus root for the
// migration tool to work without translation.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Root      string    `json:"root"`
	Remote    string    `json:"remote,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
}

// ProjectIdentity is the resolved (id, name, root, remote) tuple for
// the current working directory. Callers usually want this rather
// than the bare ProjectID() because the registry update needs every
// field together.
type ProjectIdentity struct {
	ID     string
	Name   string
	Root   string
	Remote string
}

// DetectIdentity resolves the project identity for the given root
// directory. The detection algorithm mirrors ECC's
// scripts/detect-project.sh:
//
//  1. `git remote get-url origin` from `root` (= long-lived axis,
//     survives clones across machines)
//  2. fallback to absolute `root` path (= machine-specific but
//     stable across sessions on the same laptop)
//
// Embedded credentials (= https://ghp_xxx@github.com/...) are
// stripped before hashing so a rotated token does not flip the
// project_id.
func DetectIdentity(root string) (ProjectIdentity, error) {
	if root == "" {
		return ProjectIdentity{}, errors.New("homunculus.DetectIdentity: root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ProjectIdentity{}, fmt.Errorf("homunculus.DetectIdentity: abs: %w", err)
	}
	remote := readGitRemote(abs)
	cleaned := stripCredentials(remote)
	hashInput := cleaned
	if hashInput == "" {
		hashInput = abs
	}
	id := projectIDFromHash(hashInput)
	name := projectNameFromRemoteOrRoot(cleaned, abs)
	return ProjectIdentity{ID: id, Name: name, Root: abs, Remote: cleaned}, nil
}

// ProjectID returns just the 12-hex project_id for the given root.
// Equivalent to DetectIdentity(root).ID without the surrounding
// metadata.
func ProjectID(root string) (string, error) {
	id, err := DetectIdentity(root)
	if err != nil {
		return "", err
	}
	return id.ID, nil
}

func projectIDFromHash(hashInput string) string {
	sum := sha256.Sum256([]byte(hashInput))
	hexed := hex.EncodeToString(sum[:])
	if len(hexed) < ProjectIDLen {
		return hexed
	}
	return hexed[:ProjectIDLen]
}

func projectNameFromRemoteOrRoot(remote, root string) string {
	if remote != "" {
		// strip .git suffix + extract last path segment
		base := remote
		base = strings.TrimSuffix(base, ".git")
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		base = strings.TrimSpace(base)
		if base != "" {
			return base
		}
	}
	return filepath.Base(root)
}

func readGitRemote(root string) string {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var credentialPattern = regexp.MustCompile(`://[^@/]+@`)

func stripCredentials(remote string) string {
	if remote == "" {
		return ""
	}
	return credentialPattern.ReplaceAllString(remote, "://")
}

// RegistryRW is the on-disk projects.json reader / writer. The
// guard is a process-local mutex; cross-process writes are still
// race-safe because every WriteUpsert writes to <root>.tmp then
// renames over the target (= atomic on POSIX).
type RegistryRW struct {
	layout Layout
	mu     sync.Mutex
	now    func() time.Time
}

// NewRegistryRW returns a RegistryRW pinned to layout. Tests can
// override the clock via SetClock for golden diff stability.
func NewRegistryRW(layout Layout) *RegistryRW {
	return &RegistryRW{layout: layout, now: time.Now}
}

// SetClock pins the clock the registry uses to stamp CreatedAt /
// LastSeen. Tests call this before WriteUpsert to keep golden output
// byte-stable across runs.
func (r *RegistryRW) SetClock(now func() time.Time) { r.now = now }

// Read parses projects.json. Missing file returns an empty map (=
// the registry is created on first WriteUpsert).
func (r *RegistryRW) Read() (map[string]Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readUnsafe()
}

func (r *RegistryRW) readUnsafe() (map[string]Project, error) {
	raw, err := os.ReadFile(r.layout.ProjectsJSON())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]Project{}, nil
		}
		return nil, fmt.Errorf("homunculus.RegistryRW.Read: %w", err)
	}
	out := map[string]Project{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("homunculus.RegistryRW.Read: parse: %w", err)
	}
	return out, nil
}

// WriteUpsert inserts or updates one Project. CreatedAt is set only
// on the first call for that id; LastSeen is refreshed every time.
// The write is atomic (= tmp + rename) so a half-written file never
// appears on disk.
func (r *RegistryRW) WriteUpsert(p Project) error {
	if p.ID == "" {
		return errors.New("homunculus.RegistryRW.WriteUpsert: ID empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.layout.EnsureGlobalDirs(); err != nil {
		return err
	}
	current, err := r.readUnsafe()
	if err != nil {
		return err
	}
	now := r.now().UTC()
	if existing, ok := current[p.ID]; ok && !existing.CreatedAt.IsZero() {
		p.CreatedAt = existing.CreatedAt
	} else if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.LastSeen = now
	current[p.ID] = p
	return r.flush(current)
}

func (r *RegistryRW) flush(state map[string]Project) error {
	buf, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("homunculus.RegistryRW.flush: marshal: %w", err)
	}
	target := r.layout.ProjectsJSON()
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("homunculus.RegistryRW.flush: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("homunculus.RegistryRW.flush: rename %s → %s: %w", tmp, target, err)
	}
	return nil
}
