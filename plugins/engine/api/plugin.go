package api

import (
	"context"

	pb "github.com/ikeikeikeike/bough/plugins/engine/api/proto"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// EngineProviderPlugin glues a Go EngineProvider implementation to
// go-plugin's gRPC transport. On the plugin side `Impl` is set to the
// concrete provider; on the host side `Impl` is left nil and
// GRPCClient produces a wire-backed client.
//
// Embedding plugin.Plugin{} keeps go-plugin's required interface
// methods satisfied without us having to spell them out — only the
// gRPC pair is interesting.
type EngineProviderPlugin struct {
	plugin.Plugin
	Impl EngineProvider
}

func (p *EngineProviderPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterEngineProviderServer(s, &grpcServer{impl: p.Impl})
	return nil
}

func (p *EngineProviderPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcClient{client: pb.NewEngineProviderClient(c)}, nil
}
