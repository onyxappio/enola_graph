# Independent arithmetic review

Messages msg_56076d209e88 and msg_e45362dd6df4 confirm every gate and number
against the frozen scratch evidence. Exact messages are in reviewer-delivery.json.
The primary independently checked byte identity of archived comparison, series,
window and rule against that scratch evidence; see archive-comparison.json.

This is a small engineering promotion, not statistical significance. The no-op
median gate has 9.92 ms headroom, and the worst paired no-op win is only 3.41 ms.
Counterbalanced AB/BA groups show descriptive position effects comparable to the
small treatment effect. No subset was dropped and no threshold was changed.
Initial's +0.4443% median change is not evidence of a robust slowdown or speedup.
Host load remains shared; absolute seconds cannot be compared across cohorts.

Payload byte differences (+3092 initial, +3 body, +4 structural) come entirely
from candidate run IDs being one character longer than baseline IDs, not changed
graph content. Byte-stable checkpoints mean before/after within each no-op,
not cross-arm state byte identity. summary_scans and derived_indexes vary in
both arms; the claim concerns the combined candidate, not an isolated mechanism.

The full repository test log includes 78 cached passing packages; it is not an
uncached rerun. The receipt has no wall start/end timestamps. The original pinned
manifest retains its prospective purpose text intentionally as frozen evidence;
this review and RESULTS.md record the completed outcome. A pair receipt's single
quiet_ack_message_id is not whole-team authorization: the four independently
verified acknowledgments in timing-window.json authorize the cohort.

Caffeinate is present throughout host samples and no sleep occurred; those
samples do not independently prove its -i argument. Boundary power evidence
proves AC and Low Power Mode status at boundaries, not continuous AC between them.
No new timing or source changes were requested by the reviewer.
