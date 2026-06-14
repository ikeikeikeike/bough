// Command bough-plugin-postgres is the Hashicorp go-plugin gRPC server
// for the PostgreSQL 16 database engine. The host (`bough`) shells out
// to this binary via internal/pluginhost.Discover and talks to it over
// a Unix-socket gRPC channel.
//
// This binary stays minimal on purpose — the real lifecycle logic
// lives in plugins/db/postgres.
package main

import (
	api "github.com/ikeikeikeike/bough/plugins/db/api"
	pgprovider "github.com/ikeikeikeike/bough/plugins/db/postgres"
	"github.com/hashicorp/go-plugin"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]plugin.Plugin{
			api.DBProviderPluginKey: &api.DBProviderPlugin{Impl: pgprovider.New()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
