package tsextractor

import (
	"bytes"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

func parseTypeScript(parser *sitter.Parser, src []byte) *sitter.Tree {
	return parser.Parse(parseInputForTypeScript(src), nil)
}

// parseInputForTypeScript keeps tree-sitter's TypeScript grammar out of a known
// ambiguity: an ImportType nested in a generic call's type arguments can be
// parsed as the JavaScript import() expression and turn the rest of the file
// into an expression tree. The analysis does not need the imported type's
// structure, but it does need the declarations and runtime expressions around
// it. Replace just that type-only `import("...")` span with a same-width type
// identifier in the parser input. Byte offsets and line numbers therefore stay
// identical, while every extractor continues to read names and source text from
// the untouched input.
//
// Runtime import() expressions are not inside a generic type-argument list and
// are left intact. Non-version query imports and all other source bytes are
// likewise unchanged.
func parseInputForTypeScript(src []byte) []byte {
	tokens := lexTypeScriptForParser(src)
	if len(tokens) == 0 {
		return src
	}

	// A generic call's type arguments have a matching `>` immediately followed
	// by its argument-list `(`. Keep these ranges explicit so import() calls in
	// runtime argument expressions never qualify as type imports.
	type typeRange struct{ first, last int }
	var ranges []typeRange
	for i, token := range tokens {
		if token.text != "<" || !couldStartGenericCall(tokens, i) {
			continue
		}
		end := matchingGenericCallClose(tokens, i)
		if end < 0 || end+1 >= len(tokens) || tokens[end+1].text != "(" {
			continue
		}
		ranges = append(ranges, typeRange{first: i, last: end})
	}
	if len(ranges) == 0 {
		return src
	}

	type span struct{ start, end int }
	var spans []span
	for i, token := range tokens {
		if token.text != "import" || i+3 >= len(tokens) || tokens[i+1].text != "(" ||
			tokens[i+2].kind != 's' || tokens[i+3].text != ")" {
			continue
		}
		insideTypeArgs := false
		for _, r := range ranges {
			if i > r.first && i < r.last {
				insideTypeArgs = true
				break
			}
		}
		if insideTypeArgs {
			spans = append(spans, span{start: token.start, end: tokens[i+3].end})
		}
	}
	if len(spans) == 0 {
		return src
	}

	out := bytes.Clone(src)
	for _, s := range spans {
		wroteType := false
		for i := s.start; i < s.end; i++ {
			switch out[i] {
			case '\n', '\r':
				// Preserve line and column locations exactly.
			default:
				if !wroteType {
					out[i] = 'T'
					wroteType = true
				} else {
					out[i] = ' '
				}
			}
		}
	}
	return out
}

type parserToken struct {
	text       string
	start, end int
	kind       byte
}

func lexTypeScriptForParser(src []byte) []parserToken {
	var out []parserToken
	for i := 0; i < len(src); {
		c := src[i]
		if isParserSpace(c) {
			i++
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			i += 2
			for i < len(src) && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < len(src) {
				i += 2
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote, start := c, i
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == quote {
					i++
					break
				}
				i++
			}
			kind := byte('s')
			if quote == '`' {
				kind = 't'
			}
			out = append(out, parserToken{text: string(src[start:i]), start: start, end: i, kind: kind})
			continue
		}
		if isParserIdentStart(c) {
			start := i
			i++
			for i < len(src) && isParserIdentContinue(src[i]) {
				i++
			}
			out = append(out, parserToken{text: string(src[start:i]), start: start, end: i, kind: 'i'})
			continue
		}
		// Keep each angle bracket separate so nested generic calls can be counted
		// even when the source spells their closing tokens as `>>`.
		out = append(out, parserToken{text: string(src[i : i+1]), start: i, end: i + 1, kind: 'p'})
		i++
	}
	return out
}

func matchingGenericCallClose(tokens []parserToken, open int) int {
	angle, paren, bracket, brace := 1, 0, 0, 0
	for i := open + 1; i < len(tokens); i++ {
		switch tokens[i].text {
		case "(":
			paren++
		case ")":
			if paren > 0 {
				paren--
			}
		case "[":
			bracket++
		case "]":
			if bracket > 0 {
				bracket--
			}
		case "{":
			brace++
		case "}":
			if brace > 0 {
				brace--
			}
		case "<":
			if paren == 0 && bracket == 0 && brace == 0 {
				angle++
			}
		case ">":
			if paren == 0 && bracket == 0 && brace == 0 {
				angle--
				if angle == 0 {
					return i
				}
			}
		}
	}
	return -1
}

func couldStartGenericCall(tokens []parserToken, open int) bool {
	if open == 0 {
		return false
	}
	prev := tokens[open-1]
	return prev.kind == 'i' || prev.text == ")" || prev.text == "]" || prev.text == ">"
}

func isParserSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isParserIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isParserIdentContinue(c byte) bool {
	return isParserIdentStart(c) || c >= '0' && c <= '9'
}
