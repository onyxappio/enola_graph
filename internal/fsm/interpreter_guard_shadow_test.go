package fsm

import (
	"github.com/enola-labs/enola/internal/facts"
	"strings"
	"testing"
)

func TestPrimaryInterpreterGuardParameterShadow(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		name := "imported"
		parameter := ""
		if shadow {
			name = "parameter-shadow"
			parameter = ", canProceed: () => boolean"
		}
		t.Run(name, func(t *testing.T) {
			src := `import { ready as canProceed } from './guards';
export type State = 'idle'; export type Event = {type:'GO'};
function enterState(s:State){return s;} function rejected(s:State){return s;}
export function dispatch(state:State,event:EventPARAM) {
 if(event.type === 'GO') { if(canProceed()) return enterState('idle'); }
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
				t.Fatalf("imported guard relation=%v shadow=%v", found, shadow)
			}
		})
	}
}
