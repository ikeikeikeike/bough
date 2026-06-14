// Command bough-plugin-redis is the Hashicorp go-plugin gRPC server
// for the Redis 7 database engine.
package main

import (
	api "github.com/ikeikeikeike/bough/plugins/db/api"
	redisprovider "github.com/ikeikeikeike/bough/plugins/db/redis"
	"github.com/hashicorp/go-plugin"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]plugin.Plugin{
			api.DBProviderPluginKey: &api.DBProviderPlugin{Impl: redisprovider.New()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
