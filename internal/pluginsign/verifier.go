// Package pluginsign verifies bough plugin binaries against the v0.6
// signing schemes (cosign keyless + minisign). It does not implement
// the cryptography itself — instead it spawns the canonical CLI
// tools (`cosign verify-blob`, `minisign -V`) so we inherit their
// supply-chain track record rather than reimplementing it.
//
// Round 4 priority A9 + A11: cosign is the GoReleaser keyless flow
// official bough releases use (GitHub Actions OIDC certificate);
// minisign is the Ed25519 self-host path docs/SIGNING.md recommends
// for solo / local / air-gapped plugin authors.
//
// v0.6.0 is fail-open when the verifier binary is missing — the
// host prints a clear message and skips enforcement so an operator
// who set `require_signed: true` without installing the tools sees
// what is missing rather than a hard refusal. v0.6.x adds a strict
// mode (= fail-close on missing verifier) for enterprise deploys.
package pluginsign

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Scheme is the canonical name of a signature scheme. We use strings
// (rather than an enum constant) so YAML config parses round-trip
// without a translation layer.
type Scheme string

const (
	SchemeCosign   Scheme = "cosign"
	SchemeMinisign Scheme = "minisign"
)

// Result records the outcome of a single verify attempt. Verified=
// true means the binary checked out; ToolMissing=true means the
// verifier executable was not on PATH (= fail-open at v0.6.0).
type Result struct {
	Scheme      Scheme
	Verified    bool
	ToolMissing bool
	Detail      string // signature path, error reason, or pass message
}

// Request packages the inputs every Verify call needs. SigPath is
// optional — when empty, we derive it from BinaryPath by appending
// the canonical extension (= `.sig` for minisign, `.bundle` for
// cosign keyless).
type Request struct {
	BinaryPath string
	SigPath    string
	PubKeyPath string // minisign only; cosign keyless uses OIDC
	Scheme     Scheme
}

// Verify runs the configured verifier against the binary. Errors are
// non-nil only for I/O failures or invalid inputs; "binary not
// verified" is reported via the Result, not the error, so a caller
// (= CLI) can decide whether to fail-open or fail-close.
func Verify(req Request) (*Result, error) {
	if req.BinaryPath == "" {
		return nil, errors.New("pluginsign.Verify: binary path is empty")
	}
	if _, err := os.Stat(req.BinaryPath); err != nil {
		return nil, fmt.Errorf("pluginsign.Verify: %w", err)
	}
	switch req.Scheme {
	case SchemeCosign:
		return verifyCosign(req), nil
	case SchemeMinisign:
		return verifyMinisign(req), nil
	default:
		return nil, fmt.Errorf("pluginsign.Verify: unknown scheme %q (try cosign or minisign)", req.Scheme)
	}
}

// verifyCosign spawns `cosign verify-blob`. The signature path
// defaults to "<binary>.bundle" (= GoReleaser keyless output) so
// the operator does not have to wire it explicitly.
func verifyCosign(req Request) *Result {
	res := &Result{Scheme: SchemeCosign}
	sig := req.SigPath
	if sig == "" {
		sig = req.BinaryPath + ".bundle"
	}
	bin, err := exec.LookPath("cosign")
	if err != nil {
		res.ToolMissing = true
		res.Detail = "cosign not on PATH; install via https://docs.sigstore.dev/system_config/installation/ to enforce"
		return res
	}
	if _, err := os.Stat(sig); err != nil {
		res.Detail = fmt.Sprintf("signature %q missing: %v", sig, err)
		return res
	}
	cmd := exec.Command(bin, "verify-blob", "--bundle", sig, req.BinaryPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		res.Detail = fmt.Sprintf("cosign verify-blob failed: %v: %s", err, strings.TrimSpace(string(out)))
		return res
	}
	res.Verified = true
	res.Detail = "cosign verify-blob succeeded"
	return res
}

// verifyMinisign spawns `minisign -V`. Minisign requires the
// signer's public key, so the caller MUST set req.PubKeyPath; we
// surface an explicit error rather than silently accepting an
// unverified binary.
func verifyMinisign(req Request) *Result {
	res := &Result{Scheme: SchemeMinisign}
	sig := req.SigPath
	if sig == "" {
		sig = req.BinaryPath + ".minisig"
	}
	if req.PubKeyPath == "" {
		res.Detail = "minisign requires --pubkey; supply the signer's public key"
		return res
	}
	bin, err := exec.LookPath("minisign")
	if err != nil {
		res.ToolMissing = true
		res.Detail = "minisign not on PATH; install via https://github.com/jedisct1/minisign to enforce"
		return res
	}
	if _, err := os.Stat(sig); err != nil {
		res.Detail = fmt.Sprintf("signature %q missing: %v", sig, err)
		return res
	}
	if _, err := os.Stat(req.PubKeyPath); err != nil {
		res.Detail = fmt.Sprintf("pubkey %q missing: %v", req.PubKeyPath, err)
		return res
	}
	cmd := exec.Command(bin, "-V", "-m", req.BinaryPath, "-x", sig, "-p", req.PubKeyPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		res.Detail = fmt.Sprintf("minisign -V failed: %v: %s", err, strings.TrimSpace(string(out)))
		return res
	}
	res.Verified = true
	res.Detail = "minisign -V succeeded"
	return res
}

// EnforceMissing returns true when `require_signed: true` should
// fail-close. v0.6.0 keeps it fail-open — see the package comment.
// v0.6.x flips this when the strict-mode flag lands.
func EnforceMissing(_ *Result) bool { return false }
