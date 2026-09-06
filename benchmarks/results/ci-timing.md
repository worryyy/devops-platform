# CI ablation ladder (verified from Jenkins API; full 13-service matrix unless noted)

| level | definition | build | result | duration |
|---|---|---|---|---|
| BASELINE extreme-cold | ALL optimizations off: per-service ephemeral go caches + buildctl prune before every build | #57 | SUCCESS | 2066s (34.4min) |
| L0 all-cold | shared go caches wiped + buildkit state wiped | #49 | SUCCESS | 597s |
| L0 all-cold | same protocol (pre-ladder verification run) | #46 | SUCCESS | 586s |
| L1 layer-cache-only | go caches wiped, buildkit warm (incl local-cache export reuse) | #50 | SUCCESS | 363s |
| L1 layer-cache-only | same | #51 | SUCCESS | 182s |
| L2 all-warm | no wipe | #41 | SUCCESS | 217s |
| L2 all-warm | no wipe | #55 | SUCCESS | 186s |
| L3 single-service | theme-only commit (impact analysis path) | #52 | SUCCESS | 129s |
| L3 single-service | same SHA pair, repeat | #56 | SUCCESS | 126s |

## Headline (all measured)

- Daily single-service change: **34.4min -> 2.1min (-94%, 16x)** (BASELINE -> L3)
- Full 13-service matrix: **34.4min -> 3.4min (-90%, 10x)** (BASELINE -> L2 median)
- Contribution isolation: shared go caches ~45s/layer cache ~215s/impact matrix cut ~60s/remaining warm floor ~2min
- L0->L2 gap (597->217s) is the pure cache effect on identical full builds; L2->L3 gap is the impact-analysis matrix cut.

Notes: #51 L1 is faster than #50 because #50's run exported the local layer cache
that #51 imported; #52/#56 confirm impact analysis reduces the matrix to exactly one
service (test services: theme / build services: theme). #54 (474s) excluded:
contaminated by a half-wiped cache state from an aborted run.
