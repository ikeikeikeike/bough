// Command bough-plugin-compose is the Hashicorp go-plugin gRPC server
// for kind: compose — an engine that wraps an existing docker-compose
// file/service instead of provisioning its own.
package main

import (
	"github.com/hashicorp/go-plugin"
	api "github.com/ikeikeikeike/bough/plugins/engine/api"
	composeprovider "github.com/ikeikeikeike/bough/plugins/engine/compose"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]plugin.Plugin{
			api.EngineProviderPluginKey: &api.EngineProviderPlugin{Impl: composeprovider.New()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
