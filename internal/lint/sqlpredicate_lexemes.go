package lint

// The bounded grammar does not admit comments inside statements or dialect
// identifier quoting. Reject them before whitespace normalization can erase
// a comment's newline, or a dollar inside an identifier can mimic a literal.
// Quotes and PostgreSQL dollar literals are skipped with their real boundaries.
func unsupportedPredicateLexeme(engine, sql string) string {
	for i := 0; i < len(sql); {
		end, kind, closed := sqlLexeme(sql, i, engine == "postgres")
		if kind != 0 {
			switch kind {
			case '-', '/':
				return "embedded SQL comment"
			case '$':
				if engine != "postgres" {
					return "dollar literal outside PostgreSQL"
				}
				if !closed {
					return "unclosed dollar literal"
				}
			default:
				if !closed {
					return "unclosed SQL quote"
				}
			}
			i = end
			continue
		}
		switch sql[i] {
		case '`':
			return "backtick identifier quoting"
		case '[':
			if engine == "postgres" && i+1 < len(sql) && sql[i+1] == ']' {
				i += 2
				continue
			}
			return "bracket identifier quoting"
		case '$':
			if i > 0 && (isSQLIdentifierByte(sql[i-1]) || sql[i-1] == '$' || sql[i-1] >= 0x80) {
				return "dollar in an unquoted identifier"
			}
		}
		i++
	}
	return ""
}
