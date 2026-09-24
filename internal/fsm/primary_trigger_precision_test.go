package fsm

import (
	"reflect"
	"testing"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestPrimaryTriggerPrecision(t *testing.T) {
	for _, tc := range []struct {
		name, condition string
		want            []string
	}{
		{name: "event only", condition: "event.type === 'GO'", want: []string{"GO"}},
		{name: "state predicate is not an event", condition: "event.type === 'GO' && snapshot.matches('ready')", want: []string{"GO"}},
		{name: "comment text is not an event", condition: "event.type === 'GO' /* 'not an event' */", want: []string{"GO"}},
		{name: "reversed operands", condition: "'GO' === event.type", want: []string{"GO"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := eventTriggers([]pathCondition{{Text: tc.condition, Branch: "true"}}, "event")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("eventTriggers(%q) = %v, want %v", tc.condition, got, tc.want)
			}
		})
	}
}

func TestPrimarySwitchCaseTriggerUsesSwitchBodyAncestry(t *testing.T) {
	src := []byte(`function settle(snapshot, result) {
  switch (result.type) {
    case 'requestPasswordReset': return enterState('checkInbox');
    default: return rejected(snapshot);
  }
}`)
	root, kinds, done := parse("test.ts", src)
	if done == nil {
		t.Fatal("parse failed")
	}
	defer done()
	var fn, ret *sitter.Node
	walk(root, func(n *sitter.Node) {
		if kinds.Of(n) == "function_declaration" {
			fn = n
		}
		if kinds.Of(n) == "return_statement" && ret == nil {
			ret = n
		}
	})
	if fn == nil || ret == nil {
		t.Fatal("fixture nodes missing")
	}
	path := branchConditions(ret, fn, "result", "snapshot", "rejected", src, kinds)
	got := eventTriggers(path, "result")
	if !reflect.DeepEqual(got, []string{"requestPasswordReset"}) {
		t.Fatalf("path=%+v trigger=%v", path, got)
	}
}

func TestPrimaryTriggerBooleanBranchPolarity(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		returnAt   int
		wantEvents []string
	}{
		{
			name:       "false inequality branch",
			source:     `function f(event) { if (event.type !== 'GO') { return rejected(); } else { return enterState('ready'); } }`,
			returnAt:   1,
			wantEvents: []string{"GO"},
		},
		{
			name:       "negated equality true branch is not the event",
			source:     `function f(event) { if (!(event.type === 'GO')) { return enterState('ready'); } }`,
			returnAt:   0,
			wantEvents: []string{},
		},
		{
			name:       "rejecting negated disjunction leaves either event",
			source:     `function f(event) { if (!(event.type === 'A' || event.type === 'B')) return rejected(); return enterState('ready'); }`,
			returnAt:   1,
			wantEvents: []string{"A", "B"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.source)
			root, kinds, done := parse("polarity.ts", src)
			if done == nil {
				t.Fatal("parse failed")
			}
			defer done()
			var fn *sitter.Node
			var returns []*sitter.Node
			walk(root, func(n *sitter.Node) {
				if kinds.Of(n) == "function_declaration" {
					fn = n
				}
				if kinds.Of(n) == "return_statement" {
					returns = append(returns, n)
				}
			})
			if fn == nil || tc.returnAt >= len(returns) {
				t.Fatalf("fixture did not contain return #%d", tc.returnAt+1)
			}
			path := branchConditions(returns[tc.returnAt], fn, "event", "", "rejected", src, kinds)
			got := eventTriggers(path, "event")
			if !reflect.DeepEqual(got, tc.wantEvents) {
				t.Fatalf("path=%+v trigger=%v, want %v", path, got, tc.wantEvents)
			}
		})
	}
}
