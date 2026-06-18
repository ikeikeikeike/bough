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

// LegacyHandshake is the v0.3.x DBProvider handshake. The host tries
// Handshake first and falls back to LegacyHandshake during the v0.4.x
// transition so v0.3.x binaries (= bough-plugin-mysql etc that have
// not been rebuilt against v0.4.0) still spawn. Removed in v0.5.0.
var LegacyHandshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "BOUGH_DB_PLUGIN",
	MagicCookieValue: "v1",
}

// EngineProviderPluginKey is the registry key under which the gRPC
// plugin is exposed; rpc.Dispense(EngineProviderPluginKey) on the
// host side and plugin.Serve({Plugins: {EngineProviderPluginKey:
// ...}}) on the plugin side must agree.
const EngineProviderPluginKey = "engine_provider"

// LegacyDBProviderPluginKey mirrors plugins/db/api.DBProviderPluginKey
// so the host's fallback handshake can dispense from v0.3.x binaries.
const LegacyDBProviderPluginKey = "db_provider"

// PluginMap registers EngineProviderPlugin under
// EngineProviderPluginKey. Both the host and the plugin pass this map
// to go-plugin so the wire format is symmetric.
var PluginMap = map[string]plugin.Plugin{
	EngineProviderPluginKey: &EngineProviderPlugin{},
}
