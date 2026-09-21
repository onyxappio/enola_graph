# Resident stage smoke (not performance acceptance)

Single run under concurrent development load, before graph-input policy integration.
Real fsnotify events, private frozen resident stage, dedicated NATS 14227 and independent
consumer. All 13 harness assertions passed: 10 zero-work idle and cold equality for
initial/body/structural. Initial includes watcher registration and coverage catch-up.

Observed initial 16.394 s; idle 0.110–0.166 ms; body 0.961 s; structural 1.136 s.
These timings are provisional. See ../resident-harness-review.md for acceptance gaps
identified during this smoke; final harness and repeated measurements must close them.
The observer exited on deliberate broker shutdown after all checks completed.
Original Product source contents restored. No ignored-input probes were run here.
