# New cohort preparation

No old timing arms copied or reused. Candidate runtime/binary, Product revision, policy, CLI scenario script, performance thresholds and AB/BA six-pair ordering are unchanged. Only prospective power-boundary checks were added; the original rejected cohort verdict is unchanged.

Power parser checks passed: AC at the 20% boundary accepted; battery source, 19%, unknown charge, missing AC profile, active AC Low Power Mode, missing mode rejected. Full synthetic summary accepted valid controls and rejected low-charge, missing boundary file, non-bracketing observation, forged power evidence, prior incomplete/overrun/graph-drift cases. Both frozen binary/clean-Product preflights passed without executing a benchmark.

Raw power observations and independently recomputed validation are required at each arm boundary, outside the measured interval. This does not prove uninterrupted AC within the arm. Existing sampling catches observation holes. System power settings remain untouched; use caffeinate -i around the runner for idle-sleep prevention only.

Pending: explicit new 65-minute quiet window from all four current participants. No timings yet.
