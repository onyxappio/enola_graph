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
