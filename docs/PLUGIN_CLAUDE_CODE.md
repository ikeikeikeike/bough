# bough as a Claude Code plugin

This repo doubles as a [Claude Code plugin](https://code.claude.com/docs/en/plugins.md)
so bough can be driven from inside a session (`/bough:*` slash commands) instead
of only from a shell. This is separate from bough's own gRPC **engine** plugins
(`bough-plugin-<kind>`, see [`PLUGIN_AUTHOR_GUIDE.md`](./PLUGIN_AUTHOR_GUIDE.md));
"plugin" here means the Claude Code kind.

## Layout

```text
.claude-plugin/
  marketplace.json   # the marketplace catalog (one plugin: bough, source "./")
  plugin.json        # the plugin manifest (name: bough)
commands/            # slash commands — one .md per /bough:<name>
  create.md remove.md list.md status.md verify.md doctor.md
  instinct-status.md instinct-list.md instinct-promote.md evolve.md config-validate.md
skills/
  using-bough/SKILL.md   # model-invoked orchestration + PATH preflight
hooks/
  hooks.json         # wires all 8 events to `bough hook handle --event <E>`
```

`plugin.json` omits the `commands` / `skills` / `hooks` fields on purpose: Claude
Code auto-discovers the default `commands/`, `skills/`, and `hooks/hooks.json`
directories at the plugin root. `version` is omitted too, so the plugin tracks
the git commit SHA (every push is a new version) rather than needing a manual
bump per command edit.

## The binary is a prerequisite, not bundled

The plugin ships markdown + JSON only. Every command shells out to `bough` on
`PATH`; the binary comes from a GitHub release / `nix` / `go install` (see the
README **Install** section). The `using-bough` skill runs a `command -v bough`
preflight and stops with install guidance if it is missing.

## The hook manifest is kept honest

`hooks/hooks.json` mirrors, verbatim, the command `bough hook install` writes
into `settings.json`: every event in `hooks.AllEvents()` mapped to
`hooks.CanonicalCommand(event)` (`bough hook handle --event <E>`). Because it is
a hand-authored static file, it could silently drift when the event set changes.

`internal/hooks/plugin_sync_test.go` is the guard: it parses the committed
`hooks/hooks.json` and fails CI if any event is missing, wires a non-canonical
command, or declares an event absent from `AllEvents()`. Update the manifest and
the canonical wiring in lockstep, or the test goes red.

## Don't double-wire

Installing the plugin wires the hooks; running `bough hook install` also wires
them into `settings.json`. Both present means every event fires twice. Use one:
for the plugin, run `bough hook uninstall` to drop the `settings.json` copy.
`bough doctor` prints a heads-up when it detects bough hooks in `settings.json`.
LLM instinct minting stays opt-in either way (`bough observer start`).
