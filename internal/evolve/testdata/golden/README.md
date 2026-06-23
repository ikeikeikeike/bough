# Golden corpus for evolve pipeline regression

This directory pins the 4-gate Go pipeline + HeuristicJudgeClient
output against a fixed synthetic input set. The test in
`golden_test.go` reads `inputs/*.json` files, runs them through
`Pipeline.Run`, and diffs the resulting Candidates against
`expected/*.json`.

The corpus deliberately exercises every Gate decision branch:

* `inputs/io-layer.json`         — three diverse triplet bundles
                                   (PASS expected)
* `inputs/duplicate-cluster.json`— three identical-hash bundles
                                   (FAIL expected from heuristic)
* `inputs/singleton.json`        — one bundle (DOUBT expected)
* `inputs/anti-pattern.json`     — TODO / FIXME tokens
                                   (Gate 2 drop expected)

## Refreshing after intentional changes

When you change the pipeline and the diff is intentional:

    UPDATE_GOLDEN=1 go test ./internal/evolve/... -run TestGolden

This rewrites every `expected/*.json` to match the new output.
Commit the refreshed expected file alongside the code change so
the next test run pins the new behaviour.

## v0.7.2 Python parity

v0.7.1 ships only the Go-vs-Go regression corpus because the
ECC Python v3 reference output requires running ECC against
the same synthetic input (= the v0.7.2 `bough ecc import` lands
that wiring). At that point this directory grows a
`python_v3/<id>.json` sibling for cross-checking; until then the
"golden corpus" is purely a Go regression baseline.
