package lint

import "strings"

// The bounded grammar does not admit comments inside statements or dialect
// identifier quoting. Reject them before whitespace normalization can erase
// a comment's newline, or a dollar inside an identifier can mimic a literal.
// Quotes and PostgreSQL dollar literals are skipped with their real boundaries.
func unsupportedPredicateLexeme(engine, sql string) string {
	for i := 0; i < len(sql); {
		switch sql[i] {
		case '\'', '"':
			quote := sql[i]
			escapes := engine == "postgres" && quote == '\'' && i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e') && (i == 1 || !isSQLIdentifierByte(sql[i-2]))
			i++
			closed := false
			for i < len(sql) {
				if escapes && sql[i] == '\\' && i+1 < len(sql) {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return "unclosed SQL quote"
			}
		case '`':
			return "backtick identifier quoting"
		case '[':
			if engine == "postgres" && i+1 < len(sql) && sql[i+1] == ']' {
				i += 2
				continue
			}
			return "bracket identifier quoting"
		case '-', '/':
			if i+1 < len(sql) && (sql[i] == '-' && sql[i+1] == '-' || sql[i] == '/' && sql[i+1] == '*') {
				return "embedded SQL comment"
			}
			i++
		case '$':
			if i > 0 && (isSQLIdentifierByte(sql[i-1]) || sql[i-1] == '$' || sql[i-1] >= 0x80) {
				return "dollar in an unquoted identifier"
			}
			end := i + 1
			for end < len(sql) && isDollarTagByte(sql[end], end == i+1) {
				end++
			}
			if end >= len(sql) || sql[end] != '$' {
				i++
				continue
			}
			if engine != "postgres" {
				return "dollar literal outside PostgreSQL"
			}
			tag := sql[i : end+1]
			closeAt := strings.Index(sql[end+1:], tag)
			if closeAt < 0 {
				return "unclosed dollar literal"
			}
			i = end + 1 + closeAt + len(tag)
		default:
			i++
		}
	}
	return ""
}
