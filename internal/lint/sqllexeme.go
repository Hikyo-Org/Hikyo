package lint

import "strings"

// sqlLexeme skips one SQL quote, dollar literal or comment. All bounded SQL
// analyzers share these boundaries; malformed lexemes never become predicates.
func sqlLexeme(sql string, start int, postgres bool) (end int, kind byte, closed bool) {
	end = start
	switch sql[start] {
	case '\'', '"':
		kind = sql[start]
		escapes := postgres && kind == '\'' && start > 0 && (sql[start-1] == 'E' || sql[start-1] == 'e') && (start == 1 || !isSQLIdentifierByte(sql[start-2]))
		for end = start + 1; end < len(sql); end++ {
			if escapes && sql[end] == '\\' && end+1 < len(sql) {
				end++
				continue
			}
			if sql[end] == kind {
				if end+1 < len(sql) && sql[end+1] == kind {
					end++
					continue
				}
				return end + 1, kind, true
			}
		}
	case '-':
		if start+1 < len(sql) && sql[start+1] == '-' {
			end = start + 2
			for end < len(sql) && sql[end] != '\n' {
				end++
			}
			return end, '-', true
		}
	case '/':
		if start+1 < len(sql) && sql[start+1] == '*' {
			depth := 1
			for end = start + 2; end < len(sql); {
				if end+1 < len(sql) && sql[end:end+2] == "/*" {
					depth++
					end += 2
				} else if end+1 < len(sql) && sql[end:end+2] == "*/" {
					depth--
					end += 2
					if depth == 0 {
						return end, '/', true
					}
				} else {
					end++
				}
			}
			return end, '/', false
		}
	case '$':
		if start > 0 && (isSQLIdentifierByte(sql[start-1]) || sql[start-1] == '$' || sql[start-1] >= 0x80) {
			break
		}
		end = start + 1
		for end < len(sql) && isDollarTagByte(sql[end], end == start+1) {
			end++
		}
		if end < len(sql) && sql[end] == '$' {
			tag := sql[start : end+1]
			if at := strings.Index(sql[end+1:], tag); at >= 0 {
				return end + 1 + at + len(tag), '$', true
			}
			return len(sql), '$', false
		}
	}
	if kind != 0 {
		return len(sql), kind, false
	}
	return start, 0, false
}

func isDollarTagByte(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || !first && c >= '0' && c <= '9'
}

func isSQLIdentifierByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
