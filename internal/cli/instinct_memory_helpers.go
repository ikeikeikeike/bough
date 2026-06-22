package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-plugin"
	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/config"
	"github.com/ikeikeikeike/bough/internal/instinct"
	"github.com/ikeikeikeike/bough/internal/pluginsign"
	"github.com/ikeikeikeike/bough/pkg/schema"
	memapi "github.com/ikeikeikeike/bough/plugins/memory/api"
)

// isAllowlisted is the v0.5 plugin trust check. An empty allowlist
// is interpreted as "v0.5 bundled plugins only" so the default
// config path works for solo dev (the SQLite reference-fallback
// passes silently) but a third-party `bough-plugin-memory-foo` on
// PATH triggers the warn-only NOTICE every spawn. Production teams
// set allowlist explicitly per docs/SECURITY.md.
func isAllowlisted(binName string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return binName == "bough-plugin-memory-sqlite"
	}
	for _, a := range allowlist {
		if a == binName {
			return true
		}
	}
	return false
}

// enforceSigning is the v0.6.1 spawn-time gate the `require_signed`
// flag activates. When false (= v0.6 default), this is a no-op and
// the warn-only path the v0.5 allowlist check provides stays in
// charge. When true, the gate verifies the plugin binary against
// the schemes the operator accepted (cosign / minisign) and refuses
// to spawn an unverified binary that is also not on the allowlist.
//
// Fail-open conditions (= v0.6.1 keeps these explicit so flipping
// the flag without installing tooling does not lock the operator
// out of their own host):
//
//  1. The plugin binary is on `allowlist`. v0.6.1 treats allowlist
//     as the operator's "I trust this binary, do not verify"
//     signal — useful for bough's bundled SQLite reference-fallback
//     and other plugins the operator vendored themselves.
//  2. None of the configured verifier tools (cosign, minisign) are
//     on PATH. The function logs a [NOTICE] to stderr so the
//     operator sees what is missing rather than tripping over an
//     opaque spawn refusal. Strict mode (= fail-close on missing
//     tool) lands in v0.7 once the broader Bootstrap layer
//     requires it.
//
// Required environment variables for cosign keyless verify:
//
//	BOUGH_SIGNING_CERT_IDENTITY_REGEXP   regex matching the OIDC
//	                                     identity on the signing
//	                                     certificate (= the GitHub
//	                                     Actions workflow URL the
//	                                     plugin's release pipeline
//	                                     runs under)
//	BOUGH_SIGNING_CERT_OIDC_ISSUER       defaults to GitHub Actions
//	                                     OIDC issuer when unset
//	BOUGH_SIGNING_PUBKEY                 path to the minisign public
//	                                     key (required for minisign)
func enforceSigning(binPath string, cfg *config.Config) error {
	if cfg == nil || !cfg.Instinct.PluginSecurity.RequireSigned {
		return nil
	}
	binName := filepath.Base(binPath)
	if isAllowlisted(binName, cfg.Instinct.PluginSecurity.Allowlist) {
		return nil
	}
	schemes := cfg.Instinct.PluginSecurity.AcceptedSignatureSchemes
	if len(schemes) == 0 {
		schemes = []string{string(pluginsign.SchemeCosign), string(pluginsign.SchemeMinisign)}
	}
	var lastDetail string
	for _, scheme := range schemes {
		req := pluginsign.Request{
			BinaryPath:         binPath,
			Scheme:             pluginsign.Scheme(scheme),
			CertIdentity:       os.Getenv("BOUGH_SIGNING_CERT_IDENTITY"),
			CertIdentityRegexp: os.Getenv("BOUGH_SIGNING_CERT_IDENTITY_REGEXP"),
			CertOIDCIssuer:     os.Getenv("BOUGH_SIGNING_CERT_OIDC_ISSUER"),
			PubKeyPath:         os.Getenv("BOUGH_SIGNING_PUBKEY"),
		}
		res, err := pluginsign.Verify(req)
		if err != nil {
			lastDetail = err.Error()
			continue
		}
		if res.ToolMissing {
			fmt.Fprintf(os.Stderr,
				"[NOTICE] require_signed=true but %s verifier is missing on PATH; spawn-time enforcement skipped for %s. Install the verifier to enable strict mode (%s).\n",
				scheme, binName, res.Detail,
			)
			// v0.6.1: any missing verifier opens the gate. v0.7 adds a
			// `fail_close_on_missing_verifier` flag that enterprise
			// deploys can flip to refuse-on-missing instead.
			return nil
		}
		if res.Verified {
			return nil
		}
		lastDetail = res.Detail
	}
	return fmt.Errorf(
		"require_signed=true: plugin %s failed signature verification against %v (%s); add it to plugin_security.allowlist or sign it via `bough plugins verify` flow",
		binName, schemes, lastDetail,
	)
}

// discoverMemoryBackend spawns the configured memory plugin (only
// `bough-plugin-memory-sqlite` ships in v0.5; v0.6+ adds mem0 /
// Graphiti). The returned cleanup func MUST be deferred — the
// host's go-plugin client keeps the subprocess alive otherwise.
//
// Round 3 follow-up fix (HIGH #8): v0.5 honours
// `instinct.plugin_security.allowlist` in warn-only mode.
// A plugin not in the allowlist still spawns, but a stderr
// NOTICE is emitted so the user sees "untrusted third-party
// plugin running" every time. v0.6 graduates this to an enforce
// option per docs/SECURITY.md.
func discoverMemoryBackend(cfg *config.Config) (memapi.MemoryBackend, func(), string, error) {
	kind := cfg.Instinct.DefaultMemoryBackend
	if kind == "" {
		kind = "sqlite"
	}
	role := ""
	dbPath := ""
	for _, b := range cfg.MemoryBackends {
		if b.Kind == kind {
			role = b.Role
			dbPath = b.Path
			break
		}
	}
	binName := "bough-plugin-memory-" + kind
	if cfg.Instinct.PluginSecurity.UntrustedWarning && !isAllowlisted(binName, cfg.Instinct.PluginSecurity.Allowlist) {
		fmt.Fprintf(os.Stderr,
			"[WARNING] memory plugin %q is not in plugin_security.allowlist.\n"+
				"          Third-party plugins are untrusted code (see docs/SECURITY.md).\n",
			binName,
		)
	}
	binPath, err := exec.LookPath(binName)
	if err != nil {
		return nil, nil, role, fmt.Errorf("%s not found on PATH (run `make build` or install the plugin): %w", binName, err)
	}
	if err := enforceSigning(binPath, cfg); err != nil {
		return nil, nil, role, err
	}
	cmd := exec.Command(binPath)
	if dbPath != "" {
		cmd.Env = append(cmd.Environ(), "BOUGH_MEMORY_SQLITE_PATH="+dbPath)
	}
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  memapi.Handshake,
		Plugins:          memapi.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, role, fmt.Errorf("gRPC dial %s: %w", binName, err)
	}
	raw, err := rpc.Dispense(memapi.MemoryBackendPluginKey)
	if err != nil {
		client.Kill()
		return nil, nil, role, fmt.Errorf("dispense memory_backend: %w", err)
	}
	backend, ok := raw.(memapi.MemoryBackend)
	if !ok {
		client.Kill()
		return nil, nil, role, fmt.Errorf("plugin returned %T, not MemoryBackend", raw)
	}
	return backend, func() { client.Kill() }, role, nil
}

// discoverFallbackSQLite is the v0.6 Ν-1.1f counterpart to
// discoverMemoryBackend: it always spawns the SQLite reference-
// fallback regardless of cfg.Instinct.DefaultMemoryBackend so the
// coordinator's Query path can replay against a local store when
// the primary backend (mem0 / Graphiti / ...) errors. Round 4
// AI #1 + #2 mandated this split — Read fallback must hit a real
// second process so a primary outage degrades to "stale but
// available" rather than "no memory at all".
//
// The SQLite database path is read from the `kind: sqlite` entry
// in memory_backends; if none is declared we fall through to the
// plugin binary's own default. The function never reads
// cfg.Instinct.FallbackOnError — that gate lives at the call site
// (loadInstinctCoordinator) so the fallback subprocess only spawns
// when an operator wired the feature flag on.
func discoverFallbackSQLite(cfg *config.Config) (memapi.MemoryBackend, func(), error) {
	dbPath := ""
	for _, b := range cfg.MemoryBackends {
		if b.Kind == "sqlite" {
			dbPath = b.Path
			break
		}
	}
	binName := "bough-plugin-memory-sqlite"
	binPath, err := exec.LookPath(binName)
	if err != nil {
		return nil, nil, fmt.Errorf("%s not found on PATH for fallback: %w", binName, err)
	}
	if err := enforceSigning(binPath, cfg); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(binPath)
	if dbPath != "" {
		cmd.Env = append(cmd.Environ(), "BOUGH_MEMORY_SQLITE_PATH="+dbPath)
	}
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  memapi.Handshake,
		Plugins:          memapi.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
	})
	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("gRPC dial fallback sqlite: %w", err)
	}
	raw, err := rpc.Dispense(memapi.MemoryBackendPluginKey)
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense fallback sqlite: %w", err)
	}
	backend, ok := raw.(memapi.MemoryBackend)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("fallback sqlite plugin returned %T, not MemoryBackend", raw)
	}
	return backend, func() { client.Kill() }, nil
}

// loadInstinctCoordinator does the heavy lifting both instinct and
// memory subcommands need: load .bough.yaml, discover the backend,
// construct the coordinator. The returned close func disposes
// both the backend subprocess and the coordinator's events file.
//
// Round 3 LOW #18 fix: the events.jsonl path is resolved against the
// monorepo root that loadConfigAndRoot returns, NOT the CLI cwd.
// This stops `bough memory query` and `bough instinct ingest` from
// each writing to a cwd-local events.jsonl when one ran from the
// monorepo root and the other from a worktree subdirectory.
func loadInstinctCoordinator(cmd *cobra.Command) (*instinct.Coordinator, func(), error) {
	root, cfg, err := loadConfigAndRoot(cmd, "")
	if err != nil {
		return nil, nil, err
	}
	if !cfg.Instinct.Enabled {
		return nil, nil, fmt.Errorf("instinct subsystem disabled in .bough.yaml (set `instinct.enabled: true` to use)")
	}
	backend, killBackend, _, err := discoverMemoryBackend(cfg)
	if err != nil {
		return nil, nil, err
	}
	eventsPath := ".bough/memory/events.jsonl"
	for _, b := range cfg.MemoryBackends {
		if b.EventsLog != "" {
			eventsPath = b.EventsLog
			break
		}
	}
	// Anchor relative paths to the monorepo root so the writer always
	// targets the same file regardless of cwd. Absolute paths from the
	// YAML (advanced users) are honoured as-is.
	if !filepath.IsAbs(eventsPath) {
		eventsPath = filepath.Join(root, eventsPath)
	}
	// v0.6 Ν-1.1f: spawn the SQLite reference-fallback as a
	// secondary backend when the primary is something else (= mem0
	// in v0.6, Graphiti once that plugin lands) and the operator
	// opted into fallback_on_error. v0.5 only shipped SQLite so the
	// primary was already the fallback; v0.5.1 wired the coordinator
	// slot but always passed nil. Keeping the SQLite-as-primary path
	// nil avoids spawning a second copy of the same binary against
	// the same DB file.
	var (
		fallback     memapi.MemoryBackend
		killFallback func()
	)
	primaryKind := cfg.Instinct.DefaultMemoryBackend
	if primaryKind == "" {
		primaryKind = "sqlite"
	}
	if cfg.Instinct.FallbackOnError && primaryKind != "sqlite" {
		fb, kf, ferr := discoverFallbackSQLite(cfg)
		if ferr != nil {
			killBackend()
			return nil, nil, fmt.Errorf("fallback sqlite: %w", ferr)
		}
		fallback = fb
		killFallback = kf
	}
	coord, err := instinct.New(cfg, backend, filepath.Clean(eventsPath), fallback)
	if err != nil {
		killBackend()
		if killFallback != nil {
			killFallback()
		}
		return nil, nil, err
	}
	close := func() {
		_ = coord.Close()
		killBackend()
		if killFallback != nil {
			killFallback()
		}
	}
	return coord, close, nil
}

// currentScope returns the worktree-level Scope the CLI runs in.
// We derive it from the cwd's parent monorepo + the current branch
// for now; v0.6+ adds explicit --scope / --worktree-id flags.
func currentScope(cfg *config.Config, repoName, worktreeID string) schema.Scope {
	if repoName == "" && len(cfg.Repositories) > 0 {
		repoName = cfg.Repositories[0].Name
	}
	if worktreeID == "" {
		worktreeID = "default"
	}
	return schema.Scope{
		Level:      schema.ScopeWorktree,
		WorktreeID: worktreeID,
		RepoName:   repoName,
	}
}

// noopCtx returns a context that the caller can cancel; the CLI
// does not currently propagate signal handlers down into the
// subprocess but the placeholder makes future wiring trivial.
func noopCtx() context.Context { return context.Background() }
