package config

import (
	"errors"
	"fmt"
)

// validateSemantic enforces cross-field rules that go-playground/validator
// struct tags cannot express:
//
//  1. Exactly one repository must carry role:"db-provider" when at least one
//     `databases:` entry is present. The bough host cd's into that repo to
//     issue `nix run --impure '.#mysql' -- up`, so an ambiguous (>1) or
//     absent (0) provider produces an undefined launch site.
//  2. Each Database.PortRange must be a [low, high] pair with low < high so
//     the allocator never traps in an infinite probe.
//  3. Each Ports[<kind>].Range must satisfy the same low<high constraint.
//  4. Database.Kind values must be unique — spawning two `bough-plugin-mysql`
//     instances for the same worktree would clash on /tmp socket path.
//
// All semantic errors are accumulated and returned as a single joined error
// so a config-file author sees every problem at once instead of fixing them
// one-by-one across multiple runs.
func (c *Config) validateSemantic() error {
	var errs []error

	if len(c.Databases) > 0 {
		nProvider := 0
		for _, r := range c.Repositories {
			if r.Role == "db-provider" {
				nProvider++
			}
		}
		switch nProvider {
		case 0:
			errs = append(errs, errors.New("config: at least one repository must have role: db-provider when `databases:` is non-empty"))
		case 1:
			// happy path
		default:
			errs = append(errs, fmt.Errorf("config: exactly one repository may have role: db-provider, found %d", nProvider))
		}
	}

	seenKind := map[string]bool{}
	for i, db := range c.Databases {
		if seenKind[db.Kind] {
			errs = append(errs, fmt.Errorf("config: databases[%d].kind=%q is duplicated", i, db.Kind))
		}
		seenKind[db.Kind] = true
		if db.PortRange[0] <= 0 || db.PortRange[1] <= db.PortRange[0] {
			errs = append(errs, fmt.Errorf("config: databases[%d].port_range=%v must be [low,high] with 0<low<high", i, db.PortRange))
		}
	}

	for kind, pr := range c.Ports {
		if pr.Range[0] <= 0 || pr.Range[1] <= pr.Range[0] {
			errs = append(errs, fmt.Errorf("config: ports[%s].range=%v must be [low,high] with 0<low<high", kind, pr.Range))
		}
	}

	seenRepo := map[string]bool{}
	for i, r := range c.Repositories {
		if seenRepo[r.Name] {
			errs = append(errs, fmt.Errorf("config: repositories[%d].name=%q is duplicated", i, r.Name))
		}
		seenRepo[r.Name] = true
	}

	return errors.Join(errs...)
}
