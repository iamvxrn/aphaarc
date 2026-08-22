# AlphaARC — Python submission core (ARC-AGI-3 / ARC Prize 2026)

The Go code (`pkg/macro` + `cmd/alphaarc-play`) is canonical for R&D against the
**live free API**. The Kaggle competition (**ARC Prize 2026 – ARC-AGI-3**,
deadline ~2 Nov 2026) is **Python-only and runs OFFLINE** — the submission drives
the local `arc-agi` engine and cannot call the Go binary or the network. So this
package re-implements the decision core in pure-stdlib Python 3.12, 1:1 with the
Go source.

## What's here (all unit-tested, no external deps)

| module | ports | status |
|---|---|---|
| `alphaarc/mdl.py` | `pkg/macro/mdl.go` + `mdl_number.go`: Reflect/Translate/Count savings, `best_primitive`, `best_primitive_delta` (with the signed-biggest-mover fix) | ✅ tested |
| `alphaarc/correspondence.py` | `correspondence.go`: relational primitive + `CorrespondenceResidual`, registered as 4th primitive | ✅ tested |
| `alphaarc/residual.py` | `residual.go`: reflect/translate/count residual + `residual_cells` (Correspondence always appended) + clustered `residual_targets` | ✅ tested |
| `alphaarc/perception.py` | `background_color` | ✅ |
| `alphaarc/policy.py` | the model-free driveGain decision loop from `cmd/alphaarc-play` | ✅ tested |
| `alphaarc/my_agent.py` | `MyAgent(is_done, choose_action)` adapter to the arc-agi engine | ⚠️ **VERIFY LOCALLY** |

Run the tests (no pytest needed):

```
for t in mdl correspondence residual policy; do python3 python/tests/test_$t.py; done
```

Parity: the tests pin the same constants as the Go tests (e.g. Correspondence
fill 24→23, emergence selects Correspondence, both aggregation cases), so the
offline agent's decisions match the R&D agent's.

## The one un-verified piece

`my_agent.py` is thin glue whose 3 marked spots (`_grid_of`, `_state_of`,
`_click_action`) encode assumptions about the exact arc-agi API names. They match
what our Go client parses from the same engine, but must be confirmed against the
installed `arc-agi` package with `make play-local` in the starter kit. Everything
underneath it is already proven.

## Honest status

Eligibility (an offline-runnable submission) is the hard gate before any
leaderboard score — this gets us most of the way there. It does **not** yet mean
a *good* score: the agent still doesn't generalize to unseen games (the open
research problem). Getting on the board with a valid low score first de-risks the
whole pipeline; improving the score is the separate, hard work.
