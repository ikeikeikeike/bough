// Command bough-plugin-mysql is the Hashicorp go-plugin gRPC server
// for the MySQL 8.4 LTS database engine. The host (`bough`) shells out
// to this binary via internal/pluginhost.Discover and talks to it over
// a Unix-socket gRPC channel.
//
// This binary stays minimal on purpose — the real lifecycle logic
// lives in plugins/db/mysql so that future plugin authors writing
// `bough-plugin-postgres` etc. can copy the four-line plugin.Serve
// invocation and only change the imported provider.
package main

import (
	api "github.com/ikeikeikeike/bough/plugins/db/api"
	mysqlprovider "github.com/ikeikeikeike/bough/plugins/db/mysql"
	"github.com/hashicorp/go-plugin"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: api.Handshake,
		Plugins: map[string]plugin.Plugin{
			api.DBProviderPluginKey: &api.DBProviderPlugin{Impl: mysqlprovider.New()},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
