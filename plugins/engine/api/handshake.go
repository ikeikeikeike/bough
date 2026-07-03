package api

import "github.com/hashicorp/go-plugin"

// Handshake is the v0.4.0 EngineProvider magic-cookie negotiation
// between the bough host and an engine plugin. Bumping
// ProtocolVersion is the gate for backwards-incompatible engine.proto
// changes — host and plugin must match.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  2,
	MagicCookieKey:   "BOUGH_ENGINE_PLUGIN",
	MagicCookieValue: "v2",
}

// EngineProviderPluginKey is the registry key under which the gRPC
// plugin is exposed; rpc.Dispense(EngineProviderPluginKey) on the
// host side and plugin.Serve({Plugins: {EngineProviderPluginKey:
// ...}}) on the plugin side must agree.
const EngineProviderPluginKey = "engine_provider"

// PluginMap registers EngineProviderPlugin under
// EngineProviderPluginKey. Both the host and the plugin pass this map
// to go-plugin so the wire format is symmetric.
var PluginMap = map[string]plugin.Plugin{
	EngineProviderPluginKey: &EngineProviderPlugin{},
}
