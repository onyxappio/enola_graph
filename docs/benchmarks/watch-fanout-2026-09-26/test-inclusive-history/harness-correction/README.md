The first source scenario completed all eight CLI calls and both step gates,
including cold/delta/baseline equality and silent no-op, but exited1 because the
copied Stage19 final series validator still demanded11 commits/10 transitions.
SCENARIOS.json had explicitly pinned a two-commit/one-transition scenario before
this run. The validator now checks the length of that explicit pinned chain,
requires at least two distinct commits and an initial first step, and retains
all equality, no-op, ordering and completeness gates. Fifty-one gate tests pass,
including acceptance of a complete two-commit scenario and rejection of a short
run against eleven expected commits. This raw failure remains unchanged. All
three scenarios will execute in fresh roots using the corrected runner.
