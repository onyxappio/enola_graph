# Experimental transaction-local inventory collection

Not accepted or published. This is separate from the accepted Stage33 timing
candidate 3f2883b; it must not inherit that candidate's performance evidence.

The fresh profile recorded approximately 106 ms in inventory immediately after
a policy proof that already walked repository names. The prototype collects
inventory during that same proof with the ordinary engine classifier. Inventory
pruning is simulated only for its observer; it never prunes the policy proof.
Observations are discarded on any failed proof, including late failures. The
successful inventory is consumed once by the first transaction, then cleared.
There is no persistent index, schema change, cached input-content assumption or
change to replacement planning and delivery.

Root mismatch declines the optimization. Ordinary and legacy walking retain the
same classifier and root symlink behavior. Ordering follows WalkDir preorder,
including cases such as a/child.ts preceding a-/sibling.ts. Test-file collection,
explicit exclusions and hard policy exclusions retain their prior semantics.
InventoryScans counts standalone scans; InventoryFromPolicyProof counts the
inventory supplied by the proof walk (which still costs a filesystem traversal).

Focused engine, graphinput and graphsession checks passed. Coverage includes
ordered inventory equality, reference-only tests, ignored-directory changes
still refusing the proof, root mismatch, existing policy mutation regressions,
first-transaction consumption, default full-fact no-op and summary delta/cold
contracts. A deliberately disabled collector made the equality guard fail;
the implementation was restored before positive/full verification.

Full engine/graphinput/graphsession/command checks passed (session48226 exit0):
34.120/12.066/425.770/6.372 seconds respectively. See full-packages.log. Product correctness passed: seven CLI calls and all four independent consumer
gates passed, with all seven graph hashes and parsed-file counts equal to the
frozen Stage33 candidate. Initial/no-op/body/structural parsed 6752/0/21/22 files.
The no-op preserved checkpoint bytes, generation and stream sequence. This run
overlapped the full tests deliberately and establishes no timing claim.
Diagnostic profile completed: the separate inventory span disappeared, but
classification moved into the proof. CLI totals remained about two seconds;
no overall speedup is established. See fresh-profile/RESULT.md. Full repository
validation and repeated coordinated timing remain outstanding. No speed
claim is established by focused tests or the diagnostic 106 ms attribution.
