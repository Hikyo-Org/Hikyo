package lint

import (
	"strings"
)

// This file is the small, deliberately partial SQL reader behind
// CheckContainmentGates. It is NOT a SQL parser. It masks what must not be
// read as predicates (comments, quoted identifiers, string literals), then
// records parenthesis groups and the clause keywords inside them, so a gate
// condition can be judged by where it sits: in the same query scope as the
// privacy predicate, and as a required conjunct rather than one side of an OR.
//
// Residual limits, covered by the behavioural isolation tests instead:
//   - dollar-quoted and backslash-escaped (E'...') string literals are not read;
//   - an ON condition of an outer join is treated like a WHERE filter;
//   - a CASE expression, a SELECT-list expression and a NOT-negated group never
//     count as a gate, but other boolean constructs (IS TRUE, IS DISTINCT FROM,
//     function calls) around a condition are not interpreted;
//   - an OR in an enclosing query that bypasses a whole gated subquery is not seen.

// maskedGateLiterals are the string literals the gate predicates compare
// against; every other string literal is blanked.
var maskedGateLiterals = map[string]bool{"active": true, "restricted": true, "none": true, "contained": true}

// maskSQL strips `--` and (nested) `/* */` comments, blanks every string
// literal except the gate values, replaces the content of every quoted
// identifier ("x" or `x`) with an underscore, and lower-cases the rest. A
// comment marker inside a quoted identifier or a string is not a comment, and
// a predicate spelled inside a quoted name or a string is not a predicate.
func maskSQL(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			content, next := scanQuoted(sql, i)
			i = next
			switch {
			case c == '\'' && maskedGateLiterals[content]:
				b.WriteString("'" + content + "'")
			case c == '\'':
				b.WriteString("''")
			default:
				b.WriteString(string(c) + "_" + string(c))
			}
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			for depth := 0; i < len(sql); {
				switch {
				case strings.HasPrefix(sql[i:], "/*"):
					depth++
					i += 2
				case strings.HasPrefix(sql[i:], "*/"):
					depth--
					i += 2
				default:
					i++
				}
				if depth == 0 {
					break
				}
			}
			b.WriteByte(' ')
		default:
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// scanQuoted reads the quoted token starting at sql[start], a doubled quote
// being an escaped one, and returns its raw content and the index after it. An
// unterminated token runs to the end of the text.
func scanQuoted(sql string, start int) (string, int) {
	q := sql[start]
	for j := start + 1; j < len(sql); j++ {
		if sql[j] != q {
			continue
		}
		if j+1 < len(sql) && sql[j+1] == q {
			j++
			continue
		}
		return sql[start+1 : j], j + 1
	}
	return sql[start+1:], len(sql)
}

type sqlBreak struct {
	pos  int
	word string
}

type sqlGroup struct {
	start, end int
	parent     int
	subquery   bool
	negated    bool
	ors        []int
	breaks     []sqlBreak
}

type sqlScopes struct {
	groups []sqlGroup
	cases  [][2]int
}

// sqlBreakWords start a new clause (or set-operation operand) inside a group.
var sqlBreakWords = map[string]bool{
	"select": true, "from": true, "where": true, "on": true, "join": true, "using": true,
	"group": true, "having": true, "order": true, "limit": true, "returning": true,
	"set": true, "values": true, "with": true, "union": true, "intersect": true, "except": true,
}

var sqlSetOpWords = map[string]bool{"union": true, "intersect": true, "except": true}

// a clause whose conditions filter rows: only a condition in one of these counts.
var sqlFilterClauses = map[string]bool{"on": true, "where": true, "having": true}

func isWordByte(c byte) bool {
	return c == '_' || c == '.' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// scanSQLScopes reads masked text into groups. The root group is index 0.
func scanSQLScopes(text string) *sqlScopes {
	s := &sqlScopes{groups: []sqlGroup{{start: 0, end: len(text), parent: -1, subquery: true}}}
	stack := []int{0}
	var caseStarts []int
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			_, i = scanQuoted(text, i)
		case c == '(':
			next := nextWord(text, i+1)
			s.groups = append(s.groups, sqlGroup{
				start: i, end: len(text), parent: stack[len(stack)-1],
				subquery: next == "select" || next == "with" || next == "values",
				negated:  prevWord(text, i) == "not",
			})
			stack = append(stack, len(s.groups)-1)
			i++
		case c == ')':
			if len(stack) > 1 {
				s.groups[stack[len(stack)-1]].end = i + 1
				stack = stack[:len(stack)-1]
			}
			i++
		case isWordByte(c):
			j := i
			for j < len(text) && isWordByte(text[j]) {
				j++
			}
			word := text[i:j]
			top := &s.groups[stack[len(stack)-1]]
			switch {
			case word == "or":
				top.ors = append(top.ors, i)
			case word == "case":
				caseStarts = append(caseStarts, i)
			case word == "end" && len(caseStarts) > 0:
				s.cases = append(s.cases, [2]int{caseStarts[len(caseStarts)-1], j})
				caseStarts = caseStarts[:len(caseStarts)-1]
			case sqlBreakWords[word]:
				top.breaks = append(top.breaks, sqlBreak{pos: i, word: word})
			}
			i = j
		default:
			i++
		}
	}
	for _, start := range caseStarts {
		s.cases = append(s.cases, [2]int{start, len(text)})
	}
	return s
}

func nextWord(text string, from int) string {
	for from < len(text) && text[from] == ' ' {
		from++
	}
	end := from
	for end < len(text) && isWordByte(text[end]) {
		end++
	}
	return text[from:end]
}

func prevWord(text string, before int) string {
	end := before
	for end > 0 && text[end-1] == ' ' {
		end--
	}
	start := end
	for start > 0 && isWordByte(text[start-1]) {
		start--
	}
	return text[start:end]
}

// innermost is the deepest group containing pos. Groups are in start order, so
// the last one that still contains pos is the deepest.
func (s *sqlScopes) innermost(pos int) int {
	for i := len(s.groups) - 1; i > 0; i-- {
		if g := s.groups[i]; g.start <= pos && pos < g.end {
			return i
		}
	}
	return 0
}

// gateSite is where a gate predicate sits: scope is the query scope it
// belongs to (a subquery group, and the operand of a UNION within it), and
// required reports whether every row the query returns had to satisfy it.
type gateSite struct {
	scope    [2]int
	required bool
}

// site classifies the predicate that starts at pos in the masked text, which
// is text.
func (s *sqlScopes) site(text string, pos int) gateSite {
	site := gateSite{required: prevWord(text, pos) != "not"}
	for _, span := range s.cases {
		if span[0] <= pos && pos < span[1] {
			site.required = false
		}
	}
	anchor := pos
	for g := s.innermost(pos); ; g = s.groups[g].parent {
		grp := s.groups[g]
		lo, hi, kind, setOps := 0, grp.end, "", 0
		if grp.subquery {
			for _, br := range grp.breaks {
				switch {
				case br.pos <= anchor:
					kind, lo = br.word, br.pos
					if sqlSetOpWords[br.word] {
						setOps++
					}
				case br.pos > anchor && hi == grp.end:
					hi = br.pos
				}
			}
		} else {
			lo = grp.start
		}
		if grp.negated || (grp.subquery && !sqlFilterClauses[kind]) {
			site.required = false
		}
		for _, or := range grp.ors {
			if or >= lo && or < hi {
				site.required = false
			}
		}
		if grp.subquery {
			site.scope = [2]int{g, setOps}
			return site
		}
		anchor = grp.start
	}
}
