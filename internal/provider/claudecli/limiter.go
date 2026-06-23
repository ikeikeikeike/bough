// Package claudecli spawns the operator's `claude` CLI as a
// non-interactive subprocess so bough can reuse the operator's
// existing Claude Code subscription auth. The provider exists
// entirely to enforce the user constraint declared in the v0.9
// design freeze: "claude --print のみ。 Anthropic API は使わない。
// subscription 内で動かす。"
//
// Two non-obvious safety surfaces live here:
//
//  1. Anthropic env scrub. The child process must never see
//     ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN — if those are
//     present, Claude CLI silently flips to API billing. The
//     scrub is shared with internal/observe so `bough doctor`
//     warns when the operator's shell exposes the same risk.
//
//  2. Self-DoS hard limit. The operator's subscription has a
//     soft cap. Letting the observer loop call `claude --print`
//     freely would compete with the operator's interactive
//     session and could degrade it. The Limiter caps per-session
//     + per-hour calls (= 10 / 30 default) so a runaway observer
//     can never push the operator into a Pool-2 throttle.
//
// Circuit breaker rounds out the safety story: after N consecutive
// transient failures (= network / 5xx / killed subprocess), the
// provider trips to Degraded so `bough doctor` surfaces the
// regression and the observer stops calling Claude until the
// operator resets.
package claudecli

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Default limits chosen during the v0.9 design freeze (= AI #1 risk:
// self-DoS, AI #2 risk: token bleed). Tune via Provider.Limiter
// or via .bough.yaml when we wire the config plumbing in v0.9.2.
const (
	DefaultMaxCallsPerSession = 10
	DefaultMaxCallsPerHour    = 30
	DefaultCircuitBreakerN    = 3 // consecutive failures → Degraded
	DefaultCircuitCooldown    = 15 * time.Minute
)

// ErrSelfDoSLimit is returned by Limiter.Acquire when an additional
// call would exceed the per-session or per-hour budget. Callers
// surface this through bough doctor without retrying — exceeding
// the cap is intentional.
var ErrSelfDoSLimit = errors.New("claudecli: self-DoS limit exceeded")

// ErrCircuitOpen is returned by Limiter.Acquire when the breaker is
// tripped. The observer stops calling Claude until cooldown elapses
// or the operator resets via `bough doctor reset`.
var ErrCircuitOpen = errors.New("claudecli: circuit breaker open")

// Limiter enforces the self-DoS hard limit and the consecutive-
// failure circuit breaker. Safe for concurrent use across the
// observer / inject / evolve callers (= every call site goes
// through the same Limiter instance).
type Limiter struct {
	mu         sync.Mutex
	now        func() time.Time
	sessionN   int
	hourWindow []time.Time
	failures   int
	openedAt   time.Time

	MaxCallsPerSession int
	MaxCallsPerHour    int
	CircuitBreakerN    int
	CircuitCooldown    time.Duration
}

// NewLimiter returns a Limiter wired with the v0.9 defaults. Tests
// pin the clock via SetClock so consecutive Acquire calls land in a
// deterministic window.
func NewLimiter() *Limiter {
	return &Limiter{
		now:                time.Now,
		MaxCallsPerSession: DefaultMaxCallsPerSession,
		MaxCallsPerHour:    DefaultMaxCallsPerHour,
		CircuitBreakerN:    DefaultCircuitBreakerN,
		CircuitCooldown:    DefaultCircuitCooldown,
	}
}

// SetClock pins the time source. Tests use this; production never
// calls SetClock so the limiter follows wall time.
func (l *Limiter) SetClock(now func() time.Time) { l.now = now }

// Acquire reserves one slot for an outgoing `claude --print` call.
// Returns nil when the call may proceed; ErrSelfDoSLimit when the
// per-session or per-hour cap is exhausted; ErrCircuitOpen when
// the breaker has tripped recently.
func (l *Limiter) Acquire() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.circuitOpenLocked(now) {
		return fmt.Errorf("%w (opened %s ago, cooldown %s)", ErrCircuitOpen, now.Sub(l.openedAt).Truncate(time.Second), l.CircuitCooldown)
	}
	if l.MaxCallsPerSession > 0 && l.sessionN >= l.MaxCallsPerSession {
		return fmt.Errorf("%w (session=%d cap=%d)", ErrSelfDoSLimit, l.sessionN, l.MaxCallsPerSession)
	}
	l.trimHourWindowLocked(now)
	if l.MaxCallsPerHour > 0 && len(l.hourWindow) >= l.MaxCallsPerHour {
		return fmt.Errorf("%w (hour=%d cap=%d)", ErrSelfDoSLimit, len(l.hourWindow), l.MaxCallsPerHour)
	}
	l.sessionN++
	l.hourWindow = append(l.hourWindow, now)
	return nil
}

// RecordSuccess clears the consecutive-failure tally. Callers invoke
// it after the subprocess returns a usable verdict.
func (l *Limiter) RecordSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
}

// RecordFailure increments the failure tally; the circuit trips
// when failures >= CircuitBreakerN. The cooldown clock starts on
// the trip itself.
func (l *Limiter) RecordFailure() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	if l.failures >= l.CircuitBreakerN && l.openedAt.IsZero() {
		l.openedAt = l.now()
	}
}

// Reset zeroes the session counter, hour window, failure tally, and
// breaker. `bough doctor reset` calls this; tests reuse it between
// sub-cases.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sessionN = 0
	l.hourWindow = nil
	l.failures = 0
	l.openedAt = time.Time{}
}

// Snapshot returns the current limiter state for bough doctor /
// audit logging. Safe to call from any goroutine.
type Snapshot struct {
	SessionN     int
	HourN        int
	Failures     int
	CircuitOpen  bool
	OpenedAt     time.Time
	CooldownLeft time.Duration
}

// Snapshot returns the limiter's observable state. Used by
// bough doctor to render the LLM call counter line.
func (l *Limiter) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.trimHourWindowLocked(now)
	snap := Snapshot{
		SessionN:    l.sessionN,
		HourN:       len(l.hourWindow),
		Failures:    l.failures,
		CircuitOpen: l.circuitOpenLocked(now),
		OpenedAt:    l.openedAt,
	}
	if snap.CircuitOpen {
		snap.CooldownLeft = l.CircuitCooldown - now.Sub(l.openedAt)
	}
	return snap
}

func (l *Limiter) circuitOpenLocked(now time.Time) bool {
	if l.openedAt.IsZero() {
		return false
	}
	if now.Sub(l.openedAt) >= l.CircuitCooldown {
		// Cooldown elapsed → close the circuit. Failure tally
		// stays at CircuitBreakerN so a single failure re-opens
		// the breaker; the operator can manually call Reset()
		// to forgive the failure count.
		l.openedAt = time.Time{}
		return false
	}
	return true
}

func (l *Limiter) trimHourWindowLocked(now time.Time) {
	cutoff := now.Add(-time.Hour)
	idx := 0
	for idx < len(l.hourWindow) && l.hourWindow[idx].Before(cutoff) {
		idx++
	}
	if idx > 0 {
		l.hourWindow = l.hourWindow[idx:]
	}
}
