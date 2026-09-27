# Stage36 Product watch soak

Runtime 7812838; Product fba38bab93c76a4da8df58cf45fd6847a468c315; full default profile with test sources. Session 2298 exited 0.

Ten alternating same-file restoration/mutation cycles completed in 300.045 seconds (five restorations). Every cycle matched its separate cold analysis with stable inputs, parsed one file and replaced one owner. Duplicate-content writes and interval idle checks produced zero events; the watcher remained alive through these checks. Independent replay inspection confirms 12 watch generations (initial, burst, ten cycles), each with one paired Begin/End and matching batch counts.

Sampled RSS ranged from 1036560 to 1385328 KiB. This is not leak proof. Cycle elapsed times include cold analysis and validation, and are not delta latency. This shared-host run establishes no speed acceptance or multi-file/long-duration guarantee. No watcher watermark exists: quiescence is observational; exact cold equality is the correctness oracle. Top-level watch.json totals describe the initial burst only; soak.cycles and full consumer/lifecycle logs cover the additional ten cycles.

Harness extends ../watch-diagnostic/watch.py; its run.py helper, observer source and broker template are archived there. Use that helper alongside soak.py for reproduction. Existing CLI limits permit an 8 MB Begin with 8 MiB broker payload; runtime protocol is unchanged.
