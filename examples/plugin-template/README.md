# bough plugin template

Copy this directory, rename `myplugin` to your engine kind (e.g.
`cassandra`, `mongodb`, `clickhouse`, `rabbitmq`, `kafka`), and fill in
the four `TODO:` markers. The conformance suite will then verify your
plugin satisfies the bough contract end-to-end.

## Steps

```bash
cp -r examples/plugin-template ../bough-plugin-cassandra
cd ../bough-plugin-cassandra
grep -rln myplugin | xargs sed -i.bak 's/myplugin/cassandra/g; s/MyPlugin/Cassandra/g'
find . -name '*.bak' -delete
```

Then:

1. **`docker.go`** — implement the engine-specific docker bits
   (`Cmd`, `Env`, `ExposedPorts`, `Ulimits`, readiness probe). The
   bough-internal plugins under `plugins/engine/{mysql,postgres,redis,
   elasticsearch}/docker.go` are the reference.
2. **`main.go`** — the go-plugin server entry. Copy from
   `cmd/bough-plugin-mysql/main.go` and only change the imported
   provider package.
3. **`conformance_test.go`** — pick `Image`, set `ReadyTimeout` to
   match the engine's cold-start, and supply a `NativeProbe` if the
   bough stdlib helpers (`RedisPing`, `ElasticsearchGetRoot`) don't
   fit. See the `mysql` plugin for a stdlib-only handshake-byte probe
   pattern.
4. **`.github/workflows/ci.yml`** — change the matrix `plugin` value
   and the pre-pull image ref.

Once those are filled in:

```bash
go build -o bin/bough-plugin-cassandra ./cmd/bough-plugin-cassandra
docker pull cassandra:5.0
BOUGH_CONFORMANCE_PLUGIN_BIN=$(pwd)/bin/bough-plugin-cassandra \
  go test -tags=conformance -race -timeout=15m -v ./...
```

## Multi-port engines

Engines that listen on more than one TCP socket (rabbitmq AMQP +
Management, kafka broker + KRaft controller, NATS client + monitor +
cluster) declare one entry per role from `PortRangeDefault` and bind
every entry of `UpReq.Ports` in their `Up` body. Set the matching
`MainPortRole` on the conformance config so the fault tests target
the right role:

```go
conformance.Run(t, conformance.Config{
    PluginBinary: bin,
    Image:        "rabbitmq:3-management",
    MainPortRole: "amqp",          // matches PortRangeDefault["amqp"]
    ReadyTimeout: 60 * time.Second,
})
```

See [`docs/PLUGIN_AUTHOR_GUIDE.md` — Multi-port engines](../../docs/PLUGIN_AUTHOR_GUIDE.md#multi-port-engines-rabbitmq--kafka--nats--)
for the rabbitmq author's view of `PortRangeDefault`, `Up`, and
`EnvVars` shapes.

## Background reading

- [`plugins/engine/api/CONTRACT.md`](../../plugins/engine/api/CONTRACT.md) —
  the bough plugin contract every conformance assertion traces back to.
- [`docs/PLUGIN_AUTHOR_GUIDE.md`](../../docs/PLUGIN_AUTHOR_GUIDE.md) —
  how the conformance suite ergonomics work end-to-end.
- The four bough-internal plugins under `plugins/engine/` — copy
  whichever one is closest to your engine.
