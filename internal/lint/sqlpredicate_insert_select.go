package lint

import (
	"fmt"
	"regexp"
	"strings"
)

// INSERT SELECT is admitted only when the source read proves its own chains
// and each target chain column is copied from that same named source column
// (or bound through its reserved chain argument). It retains atomic parent
// state checks, unlike splitting a checked read from the insert.
func checkInsertSelect(engine string, q Query, sql string, rules map[string]TableRule) ([]string, bool) {
	m := insertSelectRe.FindStringSubmatch(sql)
	if m == nil {
		return nil, false
	}
	label := fmt.Sprintf("sqlpredicate(%s): %s", engine, q.Name)
	rule, ok := rules[strings.ToLower(m[1])]
	if !ok {
		return []string{label + ": unknown INSERT target"}, true
	}
	if rule.Class == "authn" || rule.Class == "system" {
		return []string{label + ": unsupported INSERT target class"}, true
	}
	projection := m[3]
	from := strings.Index(strings.ToUpper(maskSQLContractLiteralsAndComments(projection)), " FROM ")
	if from < 0 {
		return []string{label + ": INSERT SELECT requires a source table"}, true
	}
	values, balanced := splitSQLTop(projection[len("SELECT "):from], ",")
	columns := strings.Split(m[2], ",")
	if !balanced || len(values) != len(columns) {
		return []string{label + ": INSERT projection does not match explicit columns"}, true
	}
	bound := map[string]bool{}
	if qualifiedSelectRe.MatchString(projection) || strings.Contains(strings.ToUpper(projection), " JOIN ") {
		if findings := checkJoinedSelectWithBindings(label+"/source", projection, rules, bound); len(findings) != 0 {
			return findings, true
		}
	} else {
		if findings := checkQuery(engine, Query{Name: q.Name + "/source", SQL: projection}, rules); len(findings) != 0 {
			return findings, true
		}
		where := strings.Index(strings.ToUpper(maskSQLContractLiteralsAndComments(projection)), " WHERE ")
		if where < 0 {
			return []string{label + ": INSERT source needs bound chain predicates"}, true
		}
		conjuncts, balanced := splitSQLTop(projection[where+7:], " AND ")
		if !balanced {
			return []string{label + ": unbalanced INSERT source"}, true
		}
		for _, predicate := range conjuncts {
			if m := conjunctRe.FindStringSubmatch(strings.TrimSpace(predicate)); m != nil && m[2] == "=" && boundParameterRe.MatchString(m[3]) {
				bound["param:"+strings.ToLower(m[1])+":"+strings.ToLower(m[3])] = true
			}
		}
	}
	for _, chain := range rule.Chain {
		found := false
		for i, column := range columns {
			if !strings.EqualFold(strings.TrimSpace(column), chain) {
				continue
			}
			found = true
			value := strings.ToLower(strings.TrimSpace(values[i]))
			expected := map[string]string{"org_id": "sqlcarg_chain_org", "project_id": "sqlcarg_chain_project", "environment_id": "sqlcarg_chain_env"}[chain]
			qualified := insertChainSourceRe.FindStringSubmatch(value)
			if value == expected && !bound["param:"+chain+":"+expected] || value != expected && (qualified == nil || qualified[1] != chain || !bound[value]) {
				return []string{label + ": INSERT chain projection is not a bound source chain column"}, true
			}
		}
		if !found {
			return []string{label + ": INSERT omits chain column " + chain}, true
		}
	}
	return nil, true
}

var (
	insertSelectRe      = regexp.MustCompile(`(?i)^INSERT INTO (\w+) \(([^)]+)\) (SELECT .+)$`)
	insertChainSourceRe = regexp.MustCompile(`^\w+\.(\w+)$`)
)
