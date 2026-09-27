# Final Stage37 candidate — source550e1fa

This final/ directory governs future acceptance, superseding the outer prototype pin set. Clean build checkout02df91a; binary15f5589549dbebb0abb315f582d19ca708de137ea3efb3993ea91b04b4aa55f7. Source review msg_e17585ec2edb found no blocking defect after the directory-policy guard. Follow-up msg_7f85d656c34f confirmed governing rule/pins; outer-rule plus final-source appendix changes no threshold.

Seven final CLI calls/four checks passed; all seven graph hashes equal the same-scope Stage36 baseline. Exact final binary traced fresh no-op confirms ts_disc_alias_roots runs (16ms in this shared-host diagnostic), with zero parses, generation1→1 and unchanged exact state bytes/broker info. No timing acceptance follows from this trace. Stats.discovery_passes counts extraction work; runtime input discovery is separately counted in WorkCounters.TSDiscoveries, hence its zero summary counter does not contradict the trace.

Harness relocation: initial history attempt refused before any workload due /tmp versus /private/tmp policy-pin mismatch. History pin paths corrected and rerun separately; failure log retained externally. CLI correctness completed using identical executable/scope bytes pinned at predecessor paths; after exit, those bytes were independently rechecked and governing paths normalized. Only the source-revision appendix in the rule differed. Pre-relocation pins and explicit validation preserved. No acceptance samples existed during this correction.

Fullsuite74147 and histories39077 still live; source review and all four17:10–17:55 UTC availability ACKs complete. No timing started yet; launcher requires final correctness receipt and full-suite success.
