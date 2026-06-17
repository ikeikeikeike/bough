# bough plugin contract

This document is the canonical list of invariants a `bough-plugin-<kind>`
binary must uphold. The `bough/conformance` test suite checks every clause
mechanically against a real Docker container, so the document and the
guard tests stay in lock-step.

Plugin authors: if your plugin passes `conformance.Run(t, cfg)`, it
satisfies this contract. If a clause below describes a behaviour you
cannot simulate (e.g. you have no socket layer to preempt with a sidecar
listener), set the corresponding `Skip*` flag in `conformance.Config`
and the suite will treat the clause as not-applicable rather than
failed.

## Lifecycle

1. **`Up` creates a Docker container named `bough-<engine>-<port>`** at
   the host port the host passed in `UpReq.Port`. The container's
   internal port is engine-specific.
2. **`Up` pulls the image** declared by `req.Extras["docker.image"]` (or
   the plugin default if absent) when it is not already cached. Pull
   failures surface as a non-nil error from `Up`; the suite asserts this
   via `Fault_ImagePullFailure`.
3. **`Up` is up-or-reuse**: if a container with the canonical name is
   already running, `Up` returns nil without recreating it. The suite
   asserts this by running the full lifecycle `IdempotentCount` times
   (default 2).
4. **`Up` surfaces port conflicts** as a non-nil error. The suite preempts
   the bough-allocated port with a sidecar `net.Listen` before calling
   `Up`; the contract test passes when `Up` returns any non-nil error.
5. **`ReadyCheck` does not return true until the service accepts at
   least one protocol-level message**. A TCP listen alone is not enough
   — the official mysql, postgres, redis and elasticsearch images all
   open the TCP socket before the daemon itself is ready.
6. **`Down` is graceful within `GracefulTimeoutSec`**. After that
   deadline the plugin must SIGKILL the workload. The suite does not
   pin a maximum value but asserts that `Down` returns without error.
7. **`Cleanup` is idempotent.** A second `Cleanup` on the same
   `datadir` + `port` must return nil. The suite calls `Cleanup` twice
   in succession to enforce this.

## EnvVars

8. **Every value `EnvVars` returns is non-empty.** The suite would
   otherwise render `KEY=` into `.env.local`, which `source` then
   collapses to the empty string — a silent data-loss path.
9. **Every host:port pair `EnvVars` advertises is reachable from the
   host.** This is the v0.2.6 invariant: a value like
   `BOUGH_<ENGINE>_HOST=172.17.0.4` (a container bridge IP) passes
   plain unit tests but crashes sniffing clients (olivere/elastic,
   pgx with sniffing on, the official low-level Java client) at boot.
   The suite finds host/port pairs by two conventions:

   - paired keys `*_HOST` + `*_PORT` with the same prefix
   - URL keys `*_URL` whose `url.Parse` yields a host and explicit port

   and dials every match with a 3 s timeout.
10. **Values are shell-safe** unless the plugin declares otherwise via
    `Config.AllowShellMetachars=true`. This is the v0.2.5 invariant:
    a `(` / `&` / `;` / `$` in a value aborts bash `source .env.local`
    on the first such byte and silently empties every later `${VAR}`,
    which is how the empty-port redis URL crashed auba-api at boot.

    The mysql go-sql-driver DSN format (`root:@tcp(127.0.0.1:3306)/db`)
    legitimately contains `(`, so plugins emitting that style must
    opt in with `AllowShellMetachars=true` and the downstream rendering
    layer must escape or interpolate carefully.

## Datadir

11. **`Up` is allowed to bind-mount but not to write to `Datadir`** for
    the docker backend. The engine inside the container writes there.
    A host-side chmod 0o000 therefore does not necessarily surface as
    an `Up` error — the suite's `Fault_DatadirPermission` may be opted
    out via `SkipDatadirPermission=true` when the plugin does not own
    the write path. AssertReachable + the native probe catch the
    downstream symptom that matters anyway.

## Notes for plugin authors

- The bough host CLI auto-detects the backend at create-time and passes
  `extras["backend"]="docker"` to `Up`. The conformance suite forces the
  same default so the docker path is always exercised. Pass
  `cfg.Extras["backend"]="nix"` to verify the services-flake path
  separately (this is out of scope for the v0.3.0 release; see
  follow-up).
- The conformance suite uses `t.TempDir()` for the worktree root,
  socket dir, and datadir. On macOS Docker Desktop these resolve via
  VirtioFS to host paths the daemon can bind-mount; on Linux runners
  they are real paths owned by the test user.
- Native protocol probes live in `conformance/probes.go` (RedisPing,
  ElasticsearchGetRoot) or as build-tag-scoped helpers next to the
  plugin (mysql's handshake-byte probe in
  `plugins/db/mysql/conformance_test.go`). Plugin authors are free to
  pass their own `Config.NativeProbe` if a stronger query (`SELECT 1`,
  `cluster health=green`, etc.) is needed for their engine.
