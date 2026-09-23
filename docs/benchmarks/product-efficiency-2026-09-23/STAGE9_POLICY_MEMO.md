# Stage 9: transaction-local policy memoization

Repeated pure hard-exclusion and Git-ignore evaluations now share a memo during one identity computation. Ancestor traversal stops when that computation has already visited the directory. No cache survives the computation; policy decisions, sorted admission arrays, serialized identity inputs and digest formats remain unchanged.

The frozen original-policy oracle passed independently, including nested ignore rules, tracked overrides and explicit exclusions. New differential tests compare cached and uncached traversal. Deliberately broken ancestor traversal and coarsened cache keys were detected. The graphsession suite passed in 437.472 s; the graphinput suite passed both in the implementation checkout and during main integration. Two engine Git-hook tests failed in the plain scratch checkout and also on its pristine control; repository pre-push checks are recorded by the integration operation.

## Matched diagnostic repeat

Base 0260153 already contains shared discovery enumeration. Candidate differs in one production file, internal/graphinput/policy.go. Both use Go 1.27.1 and identical trimpath builds against Product ae233c5f56959ce5852c8381edd6cb472c4b9f95. Five alternating steady pairs, fresh CLI, file sink:

| Measure | Control median (range), s | Candidate median (range), s |
|---|---:|---:|
| Full no-op process | 1.880 (1.805–1.949) | 1.783 (1.700–1.791) |
| resolve_graph_target | 0.571 (0.545–0.581) | 0.440 (0.427–0.456) |
| graph_session_run | 1.293 (1.238–1.356) | 1.321 (1.257–1.327) |

All five candidate walls and target-resolution times beat their paired controls. This is a diagnostic reduction of 0.097 s in median wall time (5.2%), not final acceptance. Unrelated processes remained active. Process counts of 21–25 are only a coarse concurrency indicator, not measured CPU contention; equal counts do not prove equal load. Overlapping ranges likewise do not establish that the 0.028 s session difference is purely noise.

The repeated batch added no concurrent builds or tests from this worker. An earlier batch did overlap the worker's probe build in its first pairs; its median wall reduction of 0.135 s is therefore weaker evidence. The target-resolution reduction was 0.123 s in that batch and 0.131 s in the repeat, consistent with the modified policy-build call path.

Twelve no-op runs in the repeat passed assertions for zero parses, zero new event bytes, unchanged state.json bytes and unchanged generation. Both seed states had equal policy identity, scan hash and 5,025 file records; this alone is not an exact graph-equivalence proof. Exact behavioral evidence also comes from the independent policy oracle and graphsession tests. Final combined history and NATS performance acceptance remains pending.

Candidate source SHA256: 77b70d75e9defb6f8ecea41936ce0d77669b5ef1ef5eb0314b3ac1641f42cab0.
Candidate binary SHA256: 23231528660cf249d4be92ba5a52389d96d06b28b7c3d4a0500fe4ea1517a26b.
Raw repeat logs: /tmp/enola-stage9-logs8; immutable source snapshot: /tmp/enola-stage9-candidate3-freeze.
