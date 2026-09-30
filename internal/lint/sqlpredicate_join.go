package lint

import (
	"fmt"
	"regexp"
	"strings"
)

// Joined reads are confined only when every tenant alias has its complete
// chain bound, directly or through an equality on the same chain column.
// This deliberately accepts only explicit inner joins, qualified columns,
// equality-only ON clauses and conjunctive WHERE clauses.
func checkJoinedSelect(label, sql string, rules map[string]TableRule) []string {
	return checkJoinedSelectWithBindings(label, sql, rules, map[string]bool{})
}

func checkJoinedSelectWithBindings(label, sql string, rules map[string]TableRule, bound map[string]bool) []string {
	fail := func(reason string) []string { return []string{label + ": unprovable joined read: " + reason} }
	upper := strings.ToUpper(maskSQLContractLiteralsAndComments(sql))
	if !strings.HasPrefix(upper, "SELECT ") || len(selectTokenRe.FindAllStringIndex(upper, -1)) != 1 {
		return fail("only a single SELECT is supported")
	}
	from := strings.Index(upper, " FROM ")
	where := strings.Index(upper, " WHERE ")
	if from < 0 || where < from {
		return fail("explicit FROM and WHERE required")
	}
	relation := sql[from+6 : where]
	parts := innerJoinSplitRe.Split(relation, -1)
	if len(parts) < 1 {
		return fail("explicit relation required")
	}
	aliases := map[string]TableRule{}
	add := func(text string) bool {
		m := joinedTableRe.FindStringSubmatch(strings.TrimSpace(text))
		if m == nil {
			return false
		}
		table, alias := strings.ToLower(m[1]), strings.ToLower(m[2])
		if unsupportedJoinAlias[alias] {
			return false
		}
		rule, ok := rules[table]
		if !ok || rule.Class == "authn" || rule.Class == "system" || rule.Class == "instance" {
			return false
		}
		if _, exists := aliases[alias]; exists {
			return false
		}
		aliases[alias] = rule
		return true
	}
	if !add(parts[0]) {
		return fail("unknown or unsupported root table/alias")
	}
	var equalities [][2]string
	for _, part := range parts[1:] {
		on := strings.Index(strings.ToUpper(part), " ON ")
		if on < 0 || !add(part[:on]) {
			return fail("unknown or unsupported joined table/alias")
		}
		for _, predicate := range andSplitRe.Split(part[on+4:], -1) {
			m := joinedEqualityRe.FindStringSubmatch(strings.TrimSpace(predicate))
			if m == nil {
				return fail("ON requires qualified column equalities")
			}
			equalities = append(equalities, [2]string{strings.ToLower(m[1]), strings.ToLower(m[2])})
		}
	}
	validColumn := func(col string) bool { _, ok := aliases[strings.Split(col, ".")[0]]; return ok }
	for _, eq := range equalities {
		if !validColumn(eq[0]) || !validColumn(eq[1]) {
			return fail("ON references an unknown alias")
		}
	}
	predicates := sql[where+7:]
	for _, tail := range []string{" ORDER BY ", " FOR UPDATE", " FOR SHARE", " FOR NO KEY UPDATE", " FOR KEY SHARE", " GROUP BY ", " LIMIT "} {
		if end := strings.Index(strings.ToUpper(maskSQLContractLiteralsAndComments(predicates)), tail); end >= 0 {
			predicates = predicates[:end]
		}
	}
	chain := map[string]bool{}
	for alias, rule := range aliases {
		for _, col := range rule.Chain {
			chain[alias+"."+col] = true
		}
	}
	conjuncts, balanced := splitSQLTop(predicates, " AND ")
	if !balanced {
		return fail("unbalanced WHERE")
	}
	for _, predicate := range conjuncts {
		m := joinedConjunctRe.FindStringSubmatch(strings.TrimSpace(predicate))
		if m == nil {
			if narrowPredicate(predicate, chain, validColumn) {
				continue
			}
			return fail("WHERE requires qualified conjuncts")
		}
		col, op := strings.ToLower(m[1]), m[2]
		if !validColumn(col) {
			return fail("WHERE references an unknown alias")
		}
		pair := strings.Split(col, ".")
		for _, chain := range aliases[pair[0]].Chain {
			if pair[1] == chain && (op != "=" || !boundParameterRe.MatchString(m[3])) {
				return fail("chain columns require bound equality")
			}
		}
		if op == "=" && boundParameterRe.MatchString(m[3]) {
			bound[col] = true
			bound["param:"+pair[1]+":"+strings.ToLower(m[3])] = true
		}
	}
	// Only equality of the identical column name carries a chain binding.
	// provider_id=id may establish the relationship but cannot establish scope.
	for changed := true; changed; {
		changed = false
		for _, eq := range equalities {
			if strings.Split(eq[0], ".")[1] != strings.Split(eq[1], ".")[1] {
				continue
			}
			if bound[eq[0]] && !bound[eq[1]] {
				bound[eq[1]] = true
				changed = true
			}
			if bound[eq[1]] && !bound[eq[0]] {
				bound[eq[0]] = true
				changed = true
			}
		}
	}
	for alias, rule := range aliases {
		for _, col := range rule.Chain {
			if !bound[alias+"."+col] {
				return fail(fmt.Sprintf("missing chain binding on %s.%s", alias, col))
			}
		}
	}
	return nil
}

var unsupportedJoinAlias = map[string]bool{"left": true, "right": true, "full": true, "cross": true, "natural": true, "outer": true}

var (
	qualifiedSelectRe = regexp.MustCompile(`(?i)^SELECT .+ FROM \w+ (?:AS )?\w+ WHERE `)
	innerJoinSplitRe  = regexp.MustCompile(`(?i)\s+(?:INNER\s+)?JOIN\s+`)
	joinedTableRe     = regexp.MustCompile(`(?i)^(\w+)\s+(?:AS\s+)?(\w+)$`)
	joinedEqualityRe  = regexp.MustCompile(`(?i)^(\w+\.\w+)\s*=\s*(\w+\.\w+)$`)
	joinedConjunctRe  = regexp.MustCompile(`(?i)^(\w+\.\w+)\s*(=|<>|!=|<=|>=|<|>)\s*(` + paramRe + `|'(?:[^']|'')*'|-?\d+(?:\.\d+)?|TRUE|FALSE|NULL)$`)
)
