# Fresh CLI no-op diagnostic on 7c4651d

One instrumented invocation, without a quiet performance window: outer wall 7.549 s, CLI instrumentation 7.486 s, peak process RSS 388,956,160 bytes. ParsedFiles=0, events added=0, generation2 unchanged, exact state bytes unchanged. This uses the standalone file-event diagnostic state and the existing source probe from the earlier profile, restored in finally. It is not NATS timing acceptance, not a median and not an isolated Nuxt speedup.

| Span | Seconds |
| --- | ---: |
| Input policy construction | 1.744 |
| Extractor detection (concurrent) | 1.012 |
| TS discovery | 1.120 |
| Content hashing | 0.928 |
| Inventory walk | 0.498 |
| State load | 0.350 |
| Capture contexts | 0.336 |
| Final no-publication fence | 0.318 |

Do not add nested spans or individual concurrent detector spans to their parent. The retained TS discovery observations still count17442 side reads/17454 stats; those are logical observations and do not establish actual syscall counts after the absence optimization.

## Next source-review findings

OpenAPI/AsyncAPI/gRPC independently walk during detection. Replacing those calls directly with AllNames is not yet proved equivalent: engine directory ignore pruning can differ from extractor pruning, root-name pruning differs from relative segment checks, and a flat file list loses traversal error ordering and file type information used by AsyncAPI speculative probing. Inventory reuse requires a demonstrated equivalent domain, with conservative fallback otherwise. No new index or detector behavior has been added.

Input policy check-ignore processes48606 names, while45453 tracked names are known. Simply skipping tracked names is unsafe: trackedAdmission uses whether a tracked file would be ignored to derive index-membership semantic identity, and directory overrides depend on admitted tracked descendants. Preserve those proofs when investigating batching or reuse.

The frozen six-pair candidate remains unchanged. This diagnostic guides the next optimization; it supplies no new acceptance result.
