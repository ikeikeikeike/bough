package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ikeikeikeike/bough/internal/pluginsign"
)

// newPluginsVerifyCmd wires `bough plugins verify <binary>`.
// Round 4 priority A10 made this a hard requirement before any
// future `require_signed: true` enforcement could land — operators
// need to dry-run the verification path so the first enforce
// flip-day does not surprise their CI.
//
// The CLI is intentionally thin: pluginsign.Verify owns the spawn
// of cosign / minisign, this layer just translates the result into
// a user-readable line and the correct exit status (= 0 verified,
// 1 failed, with a separate notice for tool-missing).
func newPluginsVerifyCmd() *cobra.Command {
	var (
		scheme       string
		sigPath      string
		certPath     string
		pubKey       string
		certIdentity string
		certRegexp   string
		oidcIssuer   string
	)
	cmd := &cobra.Command{
		Use:   "verify <binary>",
		Short: "Verify a bough plugin binary against the configured signing scheme",
		Args:  cobra.ExactArgs(1),
		PreRunE: func(_ *cobra.Command, _ []string) error {
			// Round 4 review #23 #5 + UX follow-up: distinguish
			// "operator forgot a flag" from "verifier rejected the
			// binary" before we even reach pluginsign.Verify.
			switch pluginsign.Scheme(scheme) {
			case pluginsign.SchemeMinisign:
				if pubKey == "" {
					return fmt.Errorf("--pubkey is required when --scheme=minisign")
				}
			case pluginsign.SchemeCosign:
				if certIdentity == "" && certRegexp == "" {
					return fmt.Errorf("--cert-identity or --cert-identity-regexp is required for cosign keyless verification (see docs/SIGNING.md)")
				}
			default:
				return fmt.Errorf("unknown --scheme %q (try cosign or minisign)", scheme)
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			req := pluginsign.Request{
				BinaryPath:         args[0],
				SigPath:            sigPath,
				CertPath:           certPath,
				PubKeyPath:         pubKey,
				Scheme:             pluginsign.Scheme(scheme),
				CertIdentity:       certIdentity,
				CertIdentityRegexp: certRegexp,
				CertOIDCIssuer:     oidcIssuer,
			}
			res, err := pluginsign.Verify(req)
			if err != nil {
				return err
			}
			if res.ToolMissing {
				fmt.Fprintf(c.OutOrStderr(), "[NOTICE] %s tool missing — %s\n", res.Scheme, res.Detail)
				fmt.Fprintln(c.OutOrStderr(), "         v0.6.0 is fail-open: enforcement is skipped when the verifier is missing.")
				return nil
			}
			if !res.Verified {
				return fmt.Errorf("%s verify failed: %s", res.Scheme, res.Detail)
			}
			fmt.Fprintf(c.OutOrStdout(), "✓ %s verified %s (%s)\n", res.Scheme, args[0], res.Detail)
			return nil
		},
	}
	cmd.Flags().StringVar(&scheme, "scheme", "cosign", "signature scheme (cosign | minisign)")
	cmd.Flags().StringVar(&sigPath, "signature", "", "explicit signature path (default: <binary>.sig then <binary>.bundle for cosign, <binary>.minisig for minisign)")
	cmd.Flags().StringVar(&certPath, "certificate", "", "cosign keyless certificate (.pem) — default derives from --signature")
	cmd.Flags().StringVar(&pubKey, "pubkey", "", "minisign public key path (required when --scheme=minisign)")
	cmd.Flags().StringVar(&certIdentity, "cert-identity", "", "cosign keyless identity (e.g. github workflow URL); required for --scheme=cosign")
	cmd.Flags().StringVar(&certRegexp, "cert-identity-regexp", "", "regex form of --cert-identity (one of the two is required for --scheme=cosign)")
	cmd.Flags().StringVar(&oidcIssuer, "cert-issuer", "", "cosign keyless OIDC issuer (default: https://token.actions.githubusercontent.com)")
	return cmd
}
