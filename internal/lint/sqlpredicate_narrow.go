package lint

import (
	"regexp"
	"strings"
)

// A non-chain condition can only narrow rows already confined by the complete
// owning chain. Its grammar excludes subqueries and arbitrary function calls.
// Parenthesized OR is allowed only among such non-chain atoms; it never counts
// as a chain binding.
func narrowPredicate(text string, chain map[string]bool, columnOK func(string) bool) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
		inner := text[1 : len(text)-1]
		for _, operator := range []string{" OR ", " AND "} {
			parts, ok := splitSQLTop(inner, operator)
			if !ok {
				return false
			}
			if len(parts) > 1 {
				for _, part := range parts {
					if !narrowPredicate(part, chain, columnOK) {
						return false
					}
				}
				return true
			}
		}
		return narrowPredicate(inner, chain, columnOK)
	}
	if optionalEmptyRe.MatchString(text) {
		return true
	}
	if m := narrowComparisonRe.FindStringSubmatch(text); m != nil {
		col := strings.ToLower(m[1])
		return !chain[col] && columnOK(col)
	}
	if m := narrowReverseComparisonRe.FindStringSubmatch(text); m != nil {
		col := strings.ToLower(m[len(m)-1])
		return !chain[col] && columnOK(col)
	}
	if m := narrowNullRe.FindStringSubmatch(text); m != nil {
		col := strings.ToLower(m[1])
		return !chain[col] && columnOK(col)
	}
	if m := narrowAnyRe.FindStringSubmatch(text); m != nil {
		col := strings.ToLower(m[1])
		return !chain[col] && columnOK(col)
	}
	if m := narrowInRe.FindStringSubmatch(text); m != nil {
		col := strings.ToLower(m[1])
		if chain[col] || !columnOK(col) {
			return false
		}
		if narrowSliceRe.MatchString(strings.TrimSpace(m[2])) {
			return true
		}
		// Commas inside quoted literal values are harmless but deliberately not
		// accepted by this small grammar; unsupported queries remain reviewable.
		for _, value := range strings.Split(m[2], ",") {
			if !narrowValueRe.MatchString(strings.TrimSpace(value)) {
				return false
			}
		}
		return true
	}
	return false
}

func splitSQLTop(text, separator string) ([]string, bool) {
	var parts []string
	depth, start := 0, 0
	quoted := false
	for i := 0; i < len(text); i++ {
		if text[i] == '\'' {
			if quoted && i+1 < len(text) && text[i+1] == '\'' {
				i++
				continue
			}
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, false
			}
		}
		if depth == 0 && i+len(separator) <= len(text) && strings.EqualFold(text[i:i+len(separator)], separator) {
			parts = append(parts, text[start:i])
			i += len(separator) - 1
			start = i + 1
		}
	}
	if depth != 0 || quoted {
		return nil, false
	}
	return append(parts, text[start:]), true
}

var (
	narrowColumn              = `(?:\w+\.)?\w+`
	narrowValue               = `(?:` + paramRe + `|'(?:[^']|'')*'|-?\d+(?:\.\d+)?|TRUE|FALSE|NULL)`
	narrowComparisonRe        = regexp.MustCompile(`(?i)^(` + narrowColumn + `)\s*(?:=|<>|!=|<=|>=|<|>)\s*` + narrowValue + `$`)
	narrowReverseComparisonRe = regexp.MustCompile(`(?i)^` + narrowValue + `\s*(?:=|<>|!=|<=|>=|<|>)\s*(` + narrowColumn + `)$`)
	narrowNullRe              = regexp.MustCompile(`(?i)^(` + narrowColumn + `) IS (?:NOT )?NULL$`)
	narrowAnyRe               = regexp.MustCompile(`(?i)^(` + narrowColumn + `)\s*=\s*ANY\((?:` + paramRe + `)(?:::text\[\])?\)$`)
	narrowInRe                = regexp.MustCompile(`(?i)^(` + narrowColumn + `) (?:NOT )?IN \((.+)\)$`)
	narrowSliceRe             = regexp.MustCompile(`(?i)^sqlc\.slice\('\w+'\)$`)
	narrowValueRe             = regexp.MustCompile(`(?i)^` + narrowValue + `$`)
	optionalEmptyRe           = regexp.MustCompile(`(?i)^(?:SQLCARG_\w+|CAST\(SQLCARG_\w+ AS TEXT\))\s*=\s*''$`)
)
