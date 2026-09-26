# Absent package manifest discovery reads — experimental bd0ef00

Reuses completed directory observations already stored by discovery to avoid opening absent package.json. No new index or persistent cache. Captured bytes (including empty captures), unknown/failed enumeration, policy refusal and case-folded names retain the original reader; present symlinks/directories are not inferred absent. The first read observation is preserved for reuse validation and transaction fences.

Independent original-reader parity covers12 filesystem/capture cases and compares read/stat/directory ledgers. Additional regressions cover new manifests invalidating retained discovery, unknown listings, legacy behavior and first-observation retention. Full TS extractor plus facts packages passed in29.625 s. Nuxt/Manifest/Discovery/Package graphsession scenarios passed in81.960 s, including cold/delta equality checks in the existing integration suite.

A fresh-CLI diagnostic returned zero parses/events, identical state bytes and unchanged generation. Its8.423 s outer wall overlaps graphsession testing, so it cannot establish speedup or regression against the earlier7.549 s diagnostic. No performance acceptance is claimed. Product NATS correctness is running separately; the existing7c4651d six-pair timing pin remains unchanged.
