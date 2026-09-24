package fsm

import (
	"strings"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestStaticRulesAccountsForUnsupportedArrayEntriesAndPushConditions(t *testing.T) {
	src := []byte("function buildRules() {\n" +
		"  const rules = [\n" +
		"    { id: 'base', from: 'A', on: 'Go', to: 'B' },\n" +
		"    ...sharedRules,\n" +
		"    dynamicRule,\n" +
		"  ];\n" +
		"  rules.push({ id: 'always', from: 'B', on: 'Go', to: 'C' });\n" +
		"  if (enabled) {\n" +
		"    rules.push({ id: 'enabled', from: 'C', on: 'Go', to: 'D' });\n" +
		"  } else {\n" +
		"    rules.push({ id: 'disabled', from: 'D', on: 'Go', to: 'E' });\n" +
		"  }\n" +
		"  rules.push(...extraRules);\n" +
		"  return rules;\n" +
		"}")
	root, kinds, done := parse("rules.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	body := functionBody(root, "buildRules", src, kinds)
	if body == nil {
		t.Fatal("could not find buildRules body")
	}
	rules, unsupported := staticRules(body, root, src, kinds)
	if unsupported != 3 {
		t.Fatalf("unsupported entries = %d, want two dynamic array entries and one spread push", unsupported)
	}
	if len(rules) != 4 {
		t.Fatalf("modeled rules = %d, want base plus three literal pushes", len(rules))
	}
	byID := map[string]staticRule{}
	for _, rule := range rules {
		id, ok := ruleString(rule, "id", src, kinds)
		if !ok {
			t.Fatalf("rule at %q has unresolved id", text(rule.site, src))
		}
		byID[id] = rule
	}
	for _, id := range []string{"base", "always", "enabled", "disabled"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing statically established rule %q", id)
		}
	}
	if byID["always"].conditional {
		t.Error("unconditional push was marked conditional")
	}
	if !byID["enabled"].conditional || !strings.Contains(byID["enabled"].condition, "enabled") {
		t.Errorf("guarded push lacks conditional evidence: %#v", byID["enabled"])
	}
	if !byID["disabled"].conditional || !strings.HasPrefix(byID["disabled"].condition, "!(") || !strings.Contains(byID["disabled"].condition, "enabled") {
		t.Errorf("else push lacks negated condition evidence: %#v", byID["disabled"])
	}
}

func TestStaticRuleArrayExpandsOnlyLiteralPerRuleHelperArguments(t *testing.T) {
	src := []byte("const makeRule = (id: string, from: string) => ({\n" +
		"  id, from, on: 'Go', to: 'Done',\n" +
		"});\n" +
		"const rules = [\n" +
		"  makeRule('known', 'Queued'),\n" +
		"  makeRule(dynamicId, 'Running'),\n" +
		"];")
	root, kinds, done := parse("rules.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	var array *sitter.Node
	walk(root, func(n *sitter.Node) {
		if array == nil && kinds.Of(n) == "array" {
			array = n
		}
	})
	if array == nil {
		t.Fatal("could not find rule array")
	}
	rules, unsupported := staticRuleArray(array, root, src, kinds)
	if unsupported != 1 {
		t.Fatalf("unsupported factory calls = %d, want 1", unsupported)
	}
	if len(rules) != 1 {
		t.Fatalf("expanded helper rules = %d, want 1", len(rules))
	}
	id, idOK := ruleString(rules[0], "id", src, kinds)
	from, fromOK := ruleString(rules[0], "from", src, kinds)
	if !idOK || id != "known" || !fromOK || from != "Queued" {
		t.Errorf("expanded literal helper = id %q (%v), from %q (%v)", id, idOK, from, fromOK)
	}
}

func TestStaticRulesUsesOnlyReturnedRuleArray(t *testing.T) {
	src := []byte(`function buildRules() {
  const unrelatedRules = [{ id: 'decoy', from: 'A', on: 'GO', to: 'B' }];
  const nested = () => [{ id: 'nested', from: 'A', on: 'GO', to: 'B' }];
  const rules = [{ id: 'actual', from: 'A', on: 'GO', to: 'B' }];
  return rules;
}`)
	root, kinds, done := parse("rules.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	body := functionBody(root, "buildRules", src, kinds)
	rules, unsupported := staticRules(body, root, src, kinds)
	if unsupported != 0 {
		t.Fatalf("unsupported returned rules = %d, want 0", unsupported)
	}
	if len(rules) != 1 {
		t.Fatalf("modeled returned rules = %d, want only the returned array entry", len(rules))
	}
	if got, ok := ruleString(rules[0], "id", src, kinds); !ok || got != "actual" {
		t.Fatalf("returned rule id = %q (%v), want actual", got, ok)
	}
}

func TestCommandTagsInResolvesReducerAndReturnedCommandsOnly(t *testing.T) {
	src := []byte(`function unrelated() {
  const startReduce = () => ({ next: null, commands: [ScanRunCommand.NestedDecoy()] });
}

const startReduce = (snapshot, event) => {
  const unused = () => ({ commands: [ScanRunCommand.Uncalled()] });
  ScanRunCommand.Discarded();
  if (!event) return { next: snapshot, commands: [] };
  return { next: snapshot, commands: event.jobs.length === 0 ? [ScanRunCommand.RequestAggregation()] : [] };
};
const rules = [{ id: 'start', reduce: startReduce }];`)
	root, kinds, done := parse("rules.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	var reducer *sitter.Node
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) == "pair" && text(n.ChildByFieldName("key"), src) == "reduce" {
			reducer = n.ChildByFieldName("value")
		}
	})
	if reducer == nil {
		t.Fatal("reducer reference missing")
	}
	got, proven := commandTagsIn(reducer, "ScanRunCommand", src, kinds)
	if !proven {
		t.Fatal("local reducer return flow was not proven")
	}
	if len(got) != 1 || got[0] != "RequestAggregation" {
		t.Fatalf("returned command tags = %v, want only conditional RequestAggregation", got)
	}
}

func TestReturnedRuleMapPreservesRulesAndTracksConditionalOverrides(t *testing.T) {
	src := []byte(`function buildRules(overrides = {}) {
  const rules = [{ id: 'queued-claim', from: 'Queued', on: 'ClaimRequested', to: 'Running', guard: checkClaim }];
  return rules.map((rule) =>
    overrides.unguardRuleIds?.has(rule.id) ? { ...rule, guard: undefined, guardIds: [] as const, guardRejection: undefined } : rule,
  );
}`)
	root, kinds, done := parse("rules.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	body := functionBody(root, "buildRules", src, kinds)
	if body == nil {
		t.Fatal("could not find buildRules body")
	}
	rules, unsupported := staticRules(body, root, src, kinds)
	if unsupported != 0 || len(rules) != 1 {
		t.Fatalf("returned rules = %d, unsupported = %d; want one preserved source rule", len(rules), unsupported)
	}
	if got, ok := ruleString(rules[0], "id", src, kinds); !ok || got != "queued-claim" {
		t.Fatalf("mapped rule identity = %q (%v), want queued-claim", got, ok)
	}
	if !rules[0].mapConditional || text(rules[0].mapOverrides["guard"], src) != "undefined" {
		t.Fatalf("guard override = %q (conditional %v), want conditional undefined", text(rules[0].mapOverrides["guard"], src), rules[0].mapConditional)
	}
	if got := rules[0].mapOverrideConditions["guard"]; got != "overrides.unguardRuleIds?.has(rule.id)" {
		t.Fatalf("guard override condition = %q, want source predicate", got)
	}
}
