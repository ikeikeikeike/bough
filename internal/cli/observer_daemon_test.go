package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikeikeikeike/bough/internal/provider/claudecli"
)

// TestWaitGone covers the SIGTERM→SIGKILL escalation gate added in
// v0.9.9: stop must be able to tell whether a signalled daemon has
// actually exited before it reports success.
func TestWaitGone(t *testing.T) {
	// our own process is alive → waitGone must report not-gone
	if waitGone(os.Getpid(), 200*time.Millisecond) {
		t.Errorf("waitGone(self) = true, want false (this process is alive)")
	}
	// an almost-certainly-absent pid → gone
	if !waitGone(2147483646, 200*time.Millisecond) {
		t.Errorf("waitGone(absent pid) = false, want true")
	}
}

// TestParseDaemonPID covers the fallback that lets `observer stop` /
// `status` find a live daemon when the pid file is stale or missing —
// the regression behind #45 (a stale observer.pid hid a running
// daemon, so stop reported "not running" and orphaned it).
func TestParseDaemonPID(t *testing.T) {
	root := "/Users/x/src/claude"
	ps := strings.Join([]string{
		"  100 /usr/bin/some-other-process --root /Users/x/src/claude",
		"  200 /Users/x/.local/bin/bough observer _run-daemon --root /Users/x/src/claude --interval 3600",
		"  300 /Users/x/.local/bin/bough observer _run-daemon --root /Users/x/src/other --interval 600",
	}, "\n")
	alive := func(int) bool { return true }
	dead := func(int) bool { return false }

	// matches the daemon line for the right root (not the other root,
	// and not the non-daemon process that merely shares the --root arg)
	if pid, ok := parseDaemonPID(ps, root, 1, alive); !ok || pid != 200 {
		t.Fatalf("got (%d,%v) want (200,true)", pid, ok)
	}

	// skips our own pid so `stop` never signals itself
	if _, ok := parseDaemonPID(ps, root, 200, alive); ok {
		t.Errorf("should skip self pid 200")
	}

	// a matching line whose process is dead is not returned
	if _, ok := parseDaemonPID(ps, root, 1, dead); ok {
		t.Errorf("should not return a dead pid")
	}

	// a root that is a prefix of the real one must not match
	if _, ok := parseDaemonPID(ps, "/Users/x/src/cla", 1, alive); ok {
		t.Errorf("prefix of a real root must not match (trailing-space guard)")
	}

	// no daemon at all → not found
	if _, ok := parseDaemonPID("  100 /usr/bin/bash\n", root, 1, alive); ok {
		t.Errorf("no daemon line should report not found")
	}
}

// TestTickOnce_HourlyCapAccumulatesAcrossTicks is the regression guard
// for the wave-4 review finding: the daemon loop used to call
// runObserverOnceQuiet directly, which spawns a subprocess that
// constructs its OWN fresh claudecli.Limiter every tick — so the
// advertised N-calls/hour self-DoS cap never accumulated across ticks
// and the daemon could fire unboundedly. tickOnce now checks a single
// limiter instance the caller is expected to hold for the daemon's
// whole lifetime; this verifies that instance actually stops firing
// once its hourly cap is reached, across repeated tickOnce calls.
func TestTickOnce_HourlyCapAccumulatesAcrossTicks(t *testing.T) {
	limiter := claudecli.NewLimiter()
	limiter.MaxCallsPerSession = 0 // daemon lifetime, not a single manual run
	limiter.MaxCallsPerHour = 2
	fixedNow := time.Now()
	limiter.SetClock(func() time.Time { return fixedNow })

	fired := 0
	runTick := func(context.Context, string) error { fired++; return nil }
	logPath := filepath.Join(t.TempDir(), "observer.log")

	for i := 0; i < 5; i++ {
		tickOnce(context.Background(), logPath, 60, "/root", limiter, runTick)
	}
	if fired != 2 {
		t.Errorf("fired = %d, want 2 (hourly cap must stop firing once limiter.MaxCallsPerHour is reached)", fired)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logBytes), "tick skipped") {
		t.Errorf("expected a \"tick skipped\" log line once the cap was hit; log:\n%s", logBytes)
	}
}

// TestTickOnce_UnboundedSessionCapWouldStillAllowManyTicks documents
// why MaxCallsPerSession is disabled (0) for the daemon's tickLimiter:
// with the default cap of 10 still enabled, an otherwise-healthy
// long-running daemon would permanently stop ticking after its 11th
// invocation, which is not what the advertised "10/session" cap means
// for a daemon (it describes a single manual run, not daemon uptime).
func TestTickOnce_UnboundedSessionCapWouldStillAllowManyTicks(t *testing.T) {
	limiter := claudecli.NewLimiter()
	limiter.MaxCallsPerSession = 0
	limiter.MaxCallsPerHour = 0 // isolate the session-cap behavior only
	fixedNow := time.Now()
	limiter.SetClock(func() time.Time { return fixedNow })

	fired := 0
	runTick := func(context.Context, string) error { fired++; return nil }
	logPath := filepath.Join(t.TempDir(), "observer.log")

	const moreThanDefaultSessionCap = claudecli.DefaultMaxCallsPerSession + 5
	for i := 0; i < moreThanDefaultSessionCap; i++ {
		tickOnce(context.Background(), logPath, 60, "/root", limiter, runTick)
	}
	if fired != moreThanDefaultSessionCap {
		t.Errorf("fired = %d, want %d (MaxCallsPerSession=0 must not cap daemon ticks)", fired, moreThanDefaultSessionCap)
	}
}

// TestTickOnce_CircuitBreakerTripsOnConsecutiveFailures is the
// regression guard for issue #86's second half: runObserverOnceQuiet
// used to swallow the pass exit status (`_ = c.Run()`), so tickOnce
// never called RecordFailure and the daemon-lifetime limiter's circuit
// breaker never opened. A failing pass must now increment the failure
// tally until the breaker trips and Acquire starts rejecting ticks.
func TestTickOnce_CircuitBreakerTripsOnConsecutiveFailures(t *testing.T) {
	limiter := claudecli.NewLimiter()
	limiter.MaxCallsPerSession = 0
	limiter.MaxCallsPerHour = 0 // isolate the circuit breaker from the caps
	fixedNow := time.Now()
	limiter.SetClock(func() time.Time { return fixedNow })

	fired := 0
	failing := func(context.Context, string) error { fired++; return errors.New("pass failed") }
	logPath := filepath.Join(t.TempDir(), "observer.log")

	for i := 0; i < claudecli.DefaultCircuitBreakerN+3; i++ {
		tickOnce(context.Background(), logPath, 60, "/root", limiter, failing)
	}
	// Once failures reach CircuitBreakerN the breaker opens and Acquire
	// rejects further ticks, so fired stops at exactly N.
	if fired != claudecli.DefaultCircuitBreakerN {
		t.Errorf("fired = %d, want %d (breaker must open after N consecutive failures and skip further ticks)",
			fired, claudecli.DefaultCircuitBreakerN)
	}
	if !limiter.Snapshot().CircuitOpen {
		t.Errorf("circuit breaker did not open after %d consecutive failures", claudecli.DefaultCircuitBreakerN)
	}
}

// TestTickOnce_ShutdownKillDoesNotTripBreaker verifies that a pass which
// fails because the daemon is shutting down (ctx cancelled → our Cancel
// SIGKILLs it) is not counted as a transient failure: a clean stop must
// not leave the breaker open or accumulate a failure tally.
func TestTickOnce_ShutdownKillDoesNotTripBreaker(t *testing.T) {
	limiter := claudecli.NewLimiter()
	limiter.MaxCallsPerSession = 0
	limiter.MaxCallsPerHour = 0
	fixedNow := time.Now()
	limiter.SetClock(func() time.Time { return fixedNow })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the daemon is shutting down before the pass returns

	killed := func(context.Context, string) error { return errors.New("signal: killed") }
	logPath := filepath.Join(t.TempDir(), "observer.log")

	for i := 0; i < claudecli.DefaultCircuitBreakerN+3; i++ {
		tickOnce(ctx, logPath, 60, "/root", limiter, killed)
	}
	if snap := limiter.Snapshot(); snap.Failures != 0 || snap.CircuitOpen {
		t.Errorf("shutdown kills recorded a failure (failures=%d, open=%v); a clean stop must not trip the breaker",
			snap.Failures, snap.CircuitOpen)
	}
}
