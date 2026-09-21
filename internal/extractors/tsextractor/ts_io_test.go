package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

func tsBoolProp(f facts.Fact, key string) bool {
	b, _ := f.Props[key].(bool)
	return b
}

func TestTsIO_DirectPrimitiveSetsIODirect(t *testing.T) {
	files := map[string]string{
		"src/net.ts": `
export function getFeed() { return fetch('/feed'); }
export function getUser() { return axios.get('/user'); }
export function readCfg() { return fs.readFileSync('/c'); }
export function beacon(d) { return navigator.sendBeacon('/b', d); }
export function openSocket() { const ws = new WebSocket('wss://x'); return ws; }
`,
	}
	got := extractAll(t, files, false)
	for _, name := range []string{"src.getFeed", "src.getUser", "src.readCfg", "src.beacon", "src.openSocket"} {
		f, ok := findFact(got, name)
		if !ok {
			t.Fatalf("missing fact %s", name)
		}
		if !tsBoolProp(f, "io_direct") {
			t.Errorf("%s: io_direct = false, want true", name)
		}
	}
}

func TestTsIO_NetworkImportBindingSetsIODirect(t *testing.T) {
	// Common shape: the network primitive is a default import from a `network` module.
	files := map[string]string{
		"src/api.ts": `
import request from '@acme/redux-tools/lib/network/request';
export function loadThing(id) { return request({ url: '/thing/' + id }); }
`,
	}
	got := extractAll(t, files, false)
	f, ok := findFact(got, "src.loadThing")
	if !ok {
		t.Fatalf("missing fact src.loadThing")
	}
	if !tsBoolProp(f, "io_direct") {
		t.Errorf("loadThing calling the network-imported `request`: io_direct = false, want true")
	}
}

func TestTsIO_NamedImportsFromNetworkModuleNotIO(t *testing.T) {
	// Regression: a network barrel/types module exports pure helpers (action-status
	// `resolved`/`rejected`, error classes, pagination utils) alongside any request
	// function. Named imports must NOT be bound as I/O sinks, or every reducer that
	// calls `resolved(...)` is falsely tagged. Also, a `/network/types` leaf must not
	// qualify as a network module at all.
	files := map[string]string{
		"src/reducer.jsx": `
import { rejected, resolved } from '@acme/redux-tools/lib/network/types';
import { assignPaginationDefaults } from '@acme/redux-tools/lib/network';
export function reducer(state, action) {
  if (action.type === resolved('X')) return assignPaginationDefaults(state);
  if (action.type === rejected('X')) return state;
  return state;
}
`,
	}
	got := extractAll(t, files, false)
	f, ok := findFact(got, "src.reducer")
	if !ok {
		t.Fatalf("missing fact src.reducer")
	}
	if tsBoolProp(f, "io_direct") || tsBoolProp(f, "performs_io") {
		t.Errorf("reducer calling pure named imports from a network module: io_direct=%v performs_io=%v, want both false",
			tsBoolProp(f, "io_direct"), tsBoolProp(f, "performs_io"))
	}
}

func TestTsIO_InMemoryCallsNotIODirect(t *testing.T) {
	files := map[string]string{
		"src/pure.ts": `
export function reduceState(list, map) {
  const x = list.update(map);
  const y = map.get('k');
  return getFetchAllUpdate(x, y);
}
function getFetchAllUpdate(a, b) { return { ...a, ...b }; }
`,
	}
	got := extractAll(t, files, false)
	for _, name := range []string{"src.reduceState", "src.getFetchAllUpdate"} {
		f, ok := findFact(got, name)
		if !ok {
			t.Fatalf("missing fact %s", name)
		}
		if tsBoolProp(f, "io_direct") {
			t.Errorf("%s: io_direct = true, want false (in-memory only)", name)
		}
		if tsBoolProp(f, "performs_io") {
			t.Errorf("%s: performs_io = true, want false (in-memory only)", name)
		}
	}
}

func TestTsIO_PerformsIOPropagatesToCaller(t *testing.T) {
	// Historical name (cache v66). Local-fact contract: the wrapper is direct I/O;
	// its caller is not tagged performs_io.
	TestTsIO_CallerDoesNotInheritPerformsIO(t)
}

func TestTsIO_CallerDoesNotInheritPerformsIO(t *testing.T) {
	files := map[string]string{
		"src/svc.ts": `
import request from 'cross-fetch';
export function wrapper(o) { return request(o); }
export function caller(id) { return wrapper({ id }); }
export function unrelated() { return 1 + 1; }
`,
	}
	got := extractAll(t, files, false)

	wrapper, _ := findFact(got, "src.wrapper")
	if !tsBoolProp(wrapper, "io_direct") || !tsBoolProp(wrapper, "performs_io") {
		t.Errorf("wrapper: io_direct=%v performs_io=%v, want both true (direct I/O)", tsBoolProp(wrapper, "io_direct"), tsBoolProp(wrapper, "performs_io"))
	}
	caller, _ := findFact(got, "src.caller")
	if tsBoolProp(caller, "io_direct") {
		t.Errorf("caller: io_direct = true, want false (only calls wrapper)")
	}
	if tsBoolProp(caller, "performs_io") {
		t.Errorf("caller: performs_io = true, want false (no transitive IO in this profile)")
	}
	unrelated, _ := findFact(got, "src.unrelated")
	if tsBoolProp(unrelated, "performs_io") {
		t.Errorf("unrelated: performs_io = true, want false")
	}
}
