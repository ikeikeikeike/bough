package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ikeikeikeike/bough/internal/config"
	engineapi "github.com/ikeikeikeike/bough/plugins/engine/api"
)

// fakeEngineProvider implements engineapi.EngineProvider with scripted
// per-phase results so startEngines' error wrapping is testable
// without spawning plugin subprocesses (#68).
type fakeEngineProvider struct {
	upErr    error
	ready    bool
	readyErr error
	envVars  map[string]string
	envErr   error
}

func (f *fakeEngineProvider) Up(context.Context, *engineapi.UpReq) error     { return f.upErr }
func (f *fakeEngineProvider) Down(context.Context, *engineapi.DownReq) error { return nil }
func (f *fakeEngineProvider) ReadyCheck(context.Context, []int, int) (bool, error) {
	return f.ready, f.readyErr
}
func (f *fakeEngineProvider) Cleanup(context.Context, string, []int) error { return nil }
func (f *fakeEngineProvider) PortRangeDefault(context.Context) (map[string]engineapi.PortRange, error) {
	return nil, nil
}

func (f *fakeEngineProvider) EnvVars(context.Context, *engineapi.EnvVarsReq) (map[string]string, error) {
	return f.envVars, f.envErr
}

// engineTestConfig declares one mysql engine with an explicit backend
// (so detectBackendIfNeeded never probes the real host) and a fixed
// ready timeout the timeout-message assertion can pin.
func engineTestConfig() *config.Config {
	return &config.Config{
		Engines: []config.Engine{{
			Kind:            "mysql",
			Backend:         "docker",
			ReadyTimeoutSec: 42,
		}},
	}
}

// TestStartEngines_WrapsPhaseErrors pins the `%s <phase>: %w` wrapping
// for every RPC phase. The EnvVars wrap was silently lost once in a
// refactor (24a4fbc, restored in bc514cf); errors.Is through the
// returned chain is the regression tripwire — a dropped %w keeps the
// message readable but breaks the unwrap.
func TestStartEngines_WrapsPhaseErrors(t *testing.T) {
	sentinel := errors.New("sentinel failure")
	tests := []struct {
		name        string
		discoverErr error
		prov        *fakeEngineProvider
		wantSubstr  string
		wantWrapped bool // sentinel must survive errors.Is through the chain
	}{
		{
			name:        "discover error",
			discoverErr: sentinel,
			wantSubstr:  "discover mysql plugin:",
			wantWrapped: true,
		},
		{
			name:        "Up error",
			prov:        &fakeEngineProvider{upErr: sentinel},
			wantSubstr:  "mysql Up:",
			wantWrapped: true,
		},
		{
			name:        "ReadyCheck error",
			prov:        &fakeEngineProvider{readyErr: sentinel},
			wantSubstr:  "mysql ReadyCheck:",
			wantWrapped: true,
		},
		{
			// (ready=false, err=nil) is a timeout: there is no underlying
			// error to wrap, so the formatted message itself is the
			// contract — and it must not leak a `%!w(<nil>)` verb.
			name:       "ReadyCheck timeout",
			prov:       &fakeEngineProvider{ready: false},
			wantSubstr: "mysql ReadyCheck: not ready within 42s",
		},
		{
			name:        "EnvVars error",
			prov:        &fakeEngineProvider{ready: true, envErr: sentinel},
			wantSubstr:  "mysql EnvVars:",
			wantWrapped: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			discover := func(kind string) (engineapi.EngineProvider, func(), error) {
				if kind != "mysql" {
					t.Fatalf("discover called with kind %q, want mysql", kind)
				}
				if tt.discoverErr != nil {
					return nil, nil, tt.discoverErr
				}
				return tt.prov, func() {}, nil
			}
			var buf bytes.Buffer
			engines, err := startEngines(
				context.Background(), &buf, engineTestConfig(),
				t.TempDir(), map[string]int{"mysql": 42001}, discover,
			)
			if err == nil {
				t.Fatal("startEngines returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("error %q missing %q", err, tt.wantSubstr)
			}
			if strings.Contains(err.Error(), "%!w") {
				t.Errorf("error %q leaked a bad format verb (nil %%w)", err)
			}
			if tt.wantWrapped && !errors.Is(err, sentinel) {
				t.Errorf("error chain lost the underlying error (dropped %%w?): %v", err)
			}
			// Contract: startEngines returns whatever it managed to bring
			// up — even on error — so the caller's defer can kill the
			// started subprocesses.
			if tt.discoverErr == nil {
				if len(engines) != 1 || engines[0].kill == nil {
					t.Errorf("engines = %d entries, want the discovered instance with its kill func", len(engines))
				}
			} else if len(engines) != 0 {
				t.Errorf("engines = %d entries on discover failure, want 0", len(engines))
			}
		})
	}
}

// TestStartEngines_HappyPathCollectsEnvVars: the success path stashes
// the provider's EnvVars on the instance (for the env-render pass) and
// logs readiness on the allocated port.
func TestStartEngines_HappyPathCollectsEnvVars(t *testing.T) {
	discover := func(string) (engineapi.EngineProvider, func(), error) {
		return &fakeEngineProvider{
			ready:   true,
			envVars: map[string]string{"BOUGH_MYSQL_PORT": "42001"},
		}, func() {}, nil
	}
	var buf bytes.Buffer
	engines, err := startEngines(
		context.Background(), &buf, engineTestConfig(),
		t.TempDir(), map[string]int{"mysql": 42001}, discover,
	)
	if err != nil {
		t.Fatalf("startEngines: %v", err)
	}
	if len(engines) != 1 {
		t.Fatalf("engines = %d, want 1", len(engines))
	}
	if got := engines[0].envVars["BOUGH_MYSQL_PORT"]; got != "42001" {
		t.Errorf("envVars not stashed on the instance: %v", engines[0].envVars)
	}
	if !strings.Contains(buf.String(), "mysql: ready on port 42001") {
		t.Errorf("missing ready log line: %q", buf.String())
	}
}
