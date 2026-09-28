package fsm

import (
	"strings"
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func TestInterpreterMixedGuardCondition(t *testing.T) {
	for _, predicate := range []string{
		"event.type === 'GO' && canProceed()",
		"canProceed() && event.type === 'GO'",
		"state.matches('idle') && canProceed()",
		"event.type === 'GO' && event.allowed",
		"event.type === 'GO' && screen === 'ready'",
		"event.type === 'GO' || canProceed()",
		"!(event.type === 'GO' && canProceed())",
		"state.matches(resolveState())",
		"message === 'event.type' && canProceed()",
	} {
		got := guardConditions([]pathCondition{{Text: predicate}}, "event", "state")
		if len(got) != 1 || got[0] != predicate {
			t.Errorf("mixed predicate lost: %q => %v", predicate, got)
		}
	}
	for _, predicate := range []string{
		"event.type === 'GO'", "'GO' === event.type", "!(event.type === 'GO')",
		"(event.type === 'GO' || event.type === 'STOP') && state.matches('idle')",
		"state.value === 'idle'", "state.matches({outer:'inner'})",
	} {
		if got := guardConditions([]pathCondition{{Text: predicate}}, "event", "state"); len(got) != 0 {
			t.Errorf("selector became guard: %q => %v", predicate, got)
		}
	}
	for _, shadow := range []bool{false, true} {
		parameter := ""
		if shadow {
			parameter = ", canProceed: () => boolean"
		}
		src := `import { ready as canProceed } from './guards';
export type State = 'idle'; export type Event = {type:'GO'};
function enterState(s:State){return s;} function rejected(s:State){return s;}
export function dispatch(state:State,event:EventPARAM) {
 if(event.type === 'GO' && canProceed()) return enterState('idle');
 return rejected(state);
}`
		sources := map[string][]byte{"machine.ts": []byte(strings.Replace(src, "PARAM", parameter, 1)), "guards.ts": []byte(`export function ready(){return true;}`)}
		spec := Spec{ID: "app", Adapter: AdapterReducerInterpreter, File: "machine.ts", Dispatcher: "dispatch", Enter: "enterState", Reject: "rejected", StateType: "State", EventType: "Event"}
		_, ff := analyzerForSources([]Spec{spec}, sources)
		found := false
		for _, f := range ff {
			for _, r := range f.Relations {
				if r.Kind == facts.RelFSMGuardCalls && r.TargetFile == "guards.ts" {
					found = true
				}
			}
		}
		if found == shadow {
			t.Errorf("mixed guard imported relation=%v shadow=%v", found, shadow)
		}
	}
}
