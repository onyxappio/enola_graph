package tsextractor

import (
	"testing"

	"github.com/enola-labs/enola/internal/facts"
)

// codata/T-046: an assignment-form or var for..in head has no `kind` field. The loop
// lexical-name scan read that missing field and dereferenced a nil node, so a
// vendored react.development.js (`for (propName in config)`) panicked the whole run.
func TestExtract_ForInWithoutLexicalKindDoesNotPanic(t *testing.T) {
	ff := extractAll(t, map[string]string{
		"src/token.ts": `export function token() { return 1 }`,
		"src/loop.ts": `
import { token } from './token'
export function assignForIn(source: Record<string, unknown>) {
  let key
  for (key in source) token()
}
export function varForIn(source: Record<string, unknown>) {
  for (var key in source) token()
}
export function assignForOf(raw: string[]) {
  let item
  for (item of raw) token()
}
`,
		"src/legacy.js": `
var token = require('./token').token
function copy(config) {
  var props = {}
  var propName
  for (propName in config) { props[propName] = config[propName] }
  return props
}
module.exports = { copy }
`,
	}, false)

	for _, name := range []string{"src.assignForIn", "src.varForIn", "src.assignForOf"} {
		f, ok := findFact(ff, name)
		if !ok {
			t.Fatalf("missing %s; facts=%v", name, factNames(ff))
		}
		// Neither form introduces a let/const binding, so the imported token stays visible.
		if !hasRelation(f, facts.RelCalls, "src.token") {
			t.Errorf("%s lost the imported token call: %+v", name, f.Relations)
		}
	}
}
