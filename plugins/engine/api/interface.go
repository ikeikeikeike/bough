// Package api carries the host-↔-plugin contract for bough engine
// plugins (mysql, postgres, redis, elasticsearch, and — from v0.4.0
// onward — rabbitmq, kafka, nats, minio, etc.). Host and plugin link
// this package; the generated gRPC stubs sit under api/proto.
//
// Renamed from plugins/db/api in v0.4.0. plugins/db/api and its
// legacy DBProvider fallback handshake (which let a v0.4.x host spawn
// a v0.3.x-built binary) existed only through the v0.4.x transition;
// v0.5.0 removed both the directory and the fallback entirely. See
// docs/MIGRATION-v0.3-to-v0.4.md.
package api

import "context"

// EngineProvider is the Go-side surface the bough host calls and that
// plugin authors implement. Every error is wrapped at the gRPC
// boundary into a string in the wire response; the Go interface
// rematerialises plain errors so the host can `errors.Is` / wrap as
// usual.
//
// Lifecycle in the typical create-then-remove flow:
//
//	PortRangeDefault — host once per kind, returns per-role ranges
//	Up               — host launches the engine on the allocated Ports
//	ReadyCheck       — host polls until every port accepts conns
//	EnvVars          — host renders .env.local snippets
//	Down             — host gracefully stops the instance
//	Cleanup          — host wipes the datadir after Down confirmed exit
type EngineProvider interface {
	Up(ctx context.Context, req *UpReq) error
	Down(ctx context.Context, req *DownReq) error
	ReadyCheck(ctx context.Context, ports []int, timeoutSec int) (bool, error)
	Cleanup(ctx context.Context, datadir string, ports []int) error
	PortRangeDefault(ctx context.Context) (map[string]PortRange, error)
	EnvVars(ctx context.Context, req *EnvVarsReq) (map[string]string, error)
}
