package tsextractor

import (
	"bytes"
	"path"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type parserSyntax uint8

const (
	parserSyntaxTypeScript parserSyntax = iota
	parserSyntaxJavaScript
)

func parseSourceForFile(parser *sitter.Parser, src []byte, file string) *sitter.Tree {
	return parseSourceWithSyntax(parser, src, sourceSyntaxForFile(file))
}

func sourceSyntaxForFile(file string) parserSyntax {
	ext := strings.ToLower(path.Ext(file))
	switch ext {
	case ".js", ".jsx", ".mjs", ".cjs", ".gjs":
		return parserSyntaxJavaScript
	default:
		return parserSyntaxTypeScript
	}
}

func parseEmbeddedScript(parser *sitter.Parser, src []byte, lang string) *sitter.Tree {
	return parseSourceWithSyntax(parser, src, sourceSyntaxForEmbeddedScript(lang))
}

func sourceSyntaxForEmbeddedScript(lang string) parserSyntax {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "js", "jsx", "javascript":
		return parserSyntaxJavaScript
	default:
		return parserSyntaxTypeScript
	}
}

func parseSourceWithSyntax(parser *sitter.Parser, src []byte, syntax parserSyntax) *sitter.Tree {
	return parser.Parse(parserInputForSyntax(src, syntax), nil)
}

func parserInputForSyntax(src []byte, syntax parserSyntax) []byte {
	if syntax == parserSyntaxJavaScript {
		// JavaScript has no generic-call type arguments. Keep every dynamic import
		// expression intact, regardless of whitespace around comparison operators.
		return src
	}
	return parseInputForTypeScript(src)
}

// parseInputForTypeScript rewrites ImportType syntax in TypeScript generic-call
// type arguments. TypeScript treats both spaced and minified
// `left < import("./runtime") > (right)` as a generic call; JavaScript treats
// both as a binary comparison and is kept on its separate parse path above.
// A parenthesized awaited import is an actual runtime expression in TypeScript,
// so it is not rewritten.
//
// The no-import fast path matters because this helper is shared by several
// TypeScript parser entry points. Candidate matching is linear in the token
// count; it never scans the remainder of the file once for every `<` token.
func parseInputForTypeScript(src []byte) []byte {
	if !bytes.Contains(src, []byte("import")) {
		return src
	}
	tokens := lexTypeScriptForParser(src)
	if len(tokens) == 0 {
		return src
	}
	ranges := genericCallTypeRanges(tokens)
	if len(ranges) == 0 {
		return src
	}
	type span struct{ start, end int }
	var spans []span
	rangeAt := 0
	active := make([]typeRange, 0, 4)
	for i, token := range tokens {
		if token.kind == 'b' { // A template interpolation is an independent expression.
			active = active[:0]
			for rangeAt < len(ranges) && ranges[rangeAt].first < i {
				rangeAt++
			}
			continue
		}
		for rangeAt < len(ranges) && ranges[rangeAt].first < i {
			active = append(active, ranges[rangeAt])
			rangeAt++
		}
		for len(active) > 0 && active[len(active)-1].last <= i {
			active = active[:len(active)-1]
		}
		if token.text != "import" || i+3 >= len(tokens) || tokens[i+1].text != "(" ||
			tokens[i+2].kind != 's' || tokens[i+3].text != ")" || len(active) == 0 {
			continue
		}
		if i > active[len(active)-1].first && i < active[len(active)-1].last && !isAwaitedRuntimeImport(tokens, i) {
			spans = append(spans, span{start: token.start, end: tokens[i+3].end})
		}
	}
	if len(spans) == 0 {
		return src
	}

	out := bytes.Clone(src)
	for _, s := range spans {
		wroteIdentifier := false
		for i := s.start; i < s.end; i++ {
			switch out[i] {
			case '\n', '\r':
				// Keep line/column locations identical for every later syntax node.
			default:
				if !wroteIdentifier {
					// `_` is a type identifier placeholder, not a synthetic local
					// variable name exposed to semantic extraction.
					out[i] = '_'
					wroteIdentifier = true
				} else {
					out[i] = ' '
				}
			}
		}
	}
	return out
}

func isAwaitedRuntimeImport(tokens []parserToken, importAt int) bool {
	return importAt > 0 && tokens[importAt-1].text == "await"
}

type parserToken struct {
	text       string
	start, end int
	kind       byte // i: identifier, s: quoted string, p: punctuation, b: template boundary
}

type typeRange struct{ first, last int }

type delimiterDepth struct{ paren, bracket, brace int }

func lexTypeScriptForParser(src []byte) []parserToken {
	var out []parserToken
	lexTypeScriptCode(src, 0, false, &out)
	return out
}

// lexTypeScriptCode emits code tokens while skipping comments, regex literals,
// and template text. `${...}` expressions are lexed recursively so type imports
// in code remain visible while import-like text in a template stays opaque.
func lexTypeScriptCode(src []byte, i int, templateExpression bool, out *[]parserToken) int {
	paren, bracket, brace := 0, 0, 0
	canStartRegex := true
	lastWord := ""
	parenControl := make([]bool, 0, 8)
	for i < len(src) {
		c := src[i]
		if isParserSpace(c) {
			i++
			continue
		}
		if templateExpression && c == '}' && brace == 0 {
			return i + 1
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
			} else {
				i = len(src)
			}
			continue
		}
		if c == '\'' || c == '"' {
			start := i
			i = scanQuotedParserString(src, i)
			*out = append(*out, parserToken{text: string(src[start:i]), start: start, end: i, kind: 's'})
			canStartRegex, lastWord = false, ""
			continue
		}
		if c == '`' {
			i = lexTemplateLiteral(src, i, out)
			canStartRegex, lastWord = false, ""
			continue
		}
		if c == '/' && canStartRegex {
			i = scanRegexParserLiteral(src, i)
			canStartRegex, lastWord = false, ""
			continue
		}
		if isParserIdentStart(c) {
			start := i
			i++
			for i < len(src) && isParserIdentContinue(src[i]) {
				i++
			}
			word := string(src[start:i])
			*out = append(*out, parserToken{text: word, start: start, end: i, kind: 'i'})
			canStartRegex = parserPrefixKeyword(word)
			lastWord = word
			continue
		}
		if c >= '0' && c <= '9' {
			start := i
			i++
			for i < len(src) && (isParserIdentContinue(src[i]) || src[i] == '.') {
				i++
			}
			*out = append(*out, parserToken{text: string(src[start:i]), start: start, end: i, kind: 'n'})
			canStartRegex, lastWord = false, ""
			continue
		}

		start := i
		text, width := parserPunctuation(src, i)
		i += width
		*out = append(*out, parserToken{text: text, start: start, end: i, kind: 'p'})
		switch text {
		case "(":
			parenControl = append(parenControl, lastWord == "if" || lastWord == "while" || lastWord == "for" || lastWord == "with" || lastWord == "switch" || lastWord == "catch")
			paren++
			canStartRegex = true
		case ")":
			if paren > 0 {
				paren--
			}
			control := false
			if len(parenControl) > 0 {
				control = parenControl[len(parenControl)-1]
				parenControl = parenControl[:len(parenControl)-1]
			}
			canStartRegex = control
		case "[":
			bracket++
			canStartRegex = true
		case "]":
			if bracket > 0 {
				bracket--
			}
			canStartRegex = false
		case "{":
			brace++
			canStartRegex = true
		case "}":
			if brace > 0 {
				brace--
			}
			// A closing block may be followed by a new expression statement.
			canStartRegex = true
		case ".", "?.":
			canStartRegex = false
		case "++", "--":
			canStartRegex = false
		default:
			canStartRegex = true
		}
		lastWord = ""
	}
	return i
}

func lexTemplateLiteral(src []byte, start int, out *[]parserToken) int {
	i := start + 1
	for i < len(src) {
		switch src[i] {
		case '\\':
			if i+1 < len(src) {
				i += 2
			} else {
				i++
			}
			continue
		case '`':
			return i + 1
		case '$':
			if i+1 < len(src) && src[i+1] == '{' {
				*out = append(*out, parserToken{text: "<template-boundary>", start: i, end: i + 2, kind: 'b'})
				i = lexTypeScriptCode(src, i+2, true, out)
				*out = append(*out, parserToken{text: "<template-boundary>", start: i, end: i, kind: 'b'})
				continue
			}
		}
		i++
	}
	return i
}

func scanQuotedParserString(src []byte, start int) int {
	quote := src[start]
	i := start + 1
	for i < len(src) {
		if src[i] == '\\' {
			if i+1 < len(src) {
				i += 2
			} else {
				i++
			}
			continue
		}
		if src[i] == quote {
			return i + 1
		}
		if src[i] == '\n' || src[i] == '\r' {
			return i
		}
		i++
	}
	return i
}

func scanRegexParserLiteral(src []byte, start int) int {
	i, inClass := start+1, false
	for i < len(src) {
		switch src[i] {
		case '\\':
			if i+1 < len(src) {
				i += 2
			} else {
				return len(src)
			}
		case '[':
			inClass = true
			i++
		case ']':
			inClass = false
			i++
		case '/':
			if inClass {
				i++
				continue
			}
			i++
			for i < len(src) && isParserIdentContinue(src[i]) {
				i++
			}
			return i
		case '\n', '\r':
			return i
		default:
			i++
		}
	}
	return i
}

func parserPunctuation(src []byte, i int) (string, int) {
	remaining := src[i:]
	for _, op := range [...]string{"...", "===", "!==", ">>>=", ">>>", "<<=", ">>=", "=>", "++", "--", "?.", "&&", "||", "??", "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=", "%=", "**", "<<"} {
		if bytes.HasPrefix(remaining, []byte(op)) {
			return op, len(op)
		}
	}
	return string(src[i : i+1]), 1
}

func genericCallTypeRanges(tokens []parserToken) []typeRange {
	stacks := make(map[delimiterDepth][]int)
	endsByOpen := make(map[int]int)
	depth := delimiterDepth{}
	for i, token := range tokens {
		if token.kind == 'b' {
			stacks = make(map[delimiterDepth][]int)
			depth = delimiterDepth{}
			continue
		}
		switch token.text {
		case "<":
			if couldStartGenericCall(tokens, i) {
				stacks[depth] = append(stacks[depth], i)
			}
		case ">":
			openings := stacks[depth]
			if len(openings) == 0 {
				break
			}
			open := openings[len(openings)-1]
			stacks[depth] = openings[:len(openings)-1]
			if i+1 < len(tokens) && tokens[i+1].text == "(" && open > 0 {
				endsByOpen[open] = i
			}
		}
		switch token.text {
		case "(":
			depth.paren++
		case ")":
			if depth.paren > 0 {
				depth.paren--
			}
		case "[":
			depth.bracket++
		case "]":
			if depth.bracket > 0 {
				depth.bracket--
			}
		case "{":
			depth.brace++
		case "}":
			if depth.brace > 0 {
				depth.brace--
			}
		}
	}
	// The first pass closes candidate ranges in nesting order. Emit them by their
	// open token so the later import scan can maintain one active interval stack.
	ranges := make([]typeRange, 0, len(endsByOpen))
	for i := range tokens {
		if end, ok := endsByOpen[i]; ok {
			ranges = append(ranges, typeRange{first: i, last: end})
		}
	}
	return ranges
}

func couldStartGenericCall(tokens []parserToken, open int) bool {
	if open == 0 {
		return false
	}
	prev := tokens[open-1]
	return prev.kind == 'i' || prev.text == ")" || prev.text == "]" || prev.text == ">"
}

func parserPrefixKeyword(word string) bool {
	switch word {
	case "return", "throw", "case", "delete", "void", "typeof", "new", "in", "of", "instanceof", "yield", "await", "else", "do", "extends", "implements", "keyof", "infer", "readonly", "as", "satisfies":
		return true
	default:
		return false
	}
}

func isParserSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isParserIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isParserIdentContinue(c byte) bool {
	return isParserIdentStart(c) || c >= '0' && c <= '9'
}
