package lint

import (
	"fmt"
	"regexp"
	"strings"
)

// A global definition may be reached through a tenant-owned binding set.
// IN preserves de-duplication and a single read snapshot. Only one membership
// subquery is supported; its SELECT and every owning-chain predicate are
// checked independently. Nullable non-chain applicability narrows that set.
func checkScopedMembership(engine string, q Query, sql string, rules map[string]TableRule) ([]string, bool) {
	m := membershipRe.FindStringSubmatch(sql)
	if m == nil {
		return nil, false
	}
	label := fmt.Sprintf("sqlpredicate(%s): %s", engine, q.Name)
	rootMask := strings.ToUpper(maskSQLContractLiteralsAndComments(m[1]))
	if len(selectTokenRe.FindAllStringIndex(rootMask, -1)) != 1 || len(fromTokenRe.FindAllStringIndex(rootMask, -1)) != 1 || strings.Contains(rootMask, " JOIN ") {
		return []string{label + ": membership root must be a single-table read"}, true
	}
	root, kind, ok := statementTarget(strings.ToUpper(m[1] + "id = SQLCARG_membership" + m[3]))
	if !ok || kind != "SELECT" || rules[strings.ToLower(root)].Class != "instance" {
		return []string{label + ": membership root must be an instance definition"}, true
	}
	if findings := checkWhere(label, m[1]+"id = SQLCARG_membership"+m[3], strings.ToUpper(m[1]+"id = SQLCARG_membership"+m[3]), nil); len(findings) != 0 {
		return findings, true
	}
	inner := m[2]
	target, kind, ok := statementTarget(strings.ToUpper(inner))
	if !ok || kind != "SELECT" {
		return []string{label + ": unrecognized membership read"}, true
	}
	rule, known := rules[strings.ToLower(target)]
	if !known || rule.Class == "instance" || rule.Class == "authn" || rule.Class == "system" {
		return []string{label + ": membership binding must be tenant-owned"}, true
	}
	invalid := false
	inner = nullableApplicabilityRe.ReplaceAllStringFunc(inner, func(predicate string) string {
		match := nullableApplicabilityRe.FindStringSubmatch(predicate)
		if !strings.EqualFold(match[1], match[2]) {
			invalid = true
			return predicate
		}
		for _, col := range rule.Chain {
			if strings.EqualFold(col, match[1]) {
				invalid = true
				return predicate
			}
		}
		return match[2] + "=" + match[3]
	})
	if invalid {
		return []string{label + ": nullable applicability cannot replace a chain binding"}, true
	}
	findings := checkQuery(engine, Query{Name: q.Name + "/membership", SQL: inner}, rules)
	return findings, true
}

var (
	fromTokenRe             = regexp.MustCompile(`\bFROM\b`)
	membershipRe            = regexp.MustCompile(`(?i)^(SELECT [^()]+ FROM \w+ WHERE (?:\w+\s*=\s*(?:\?|\$\d+|SQLCARG_\w+) AND )?)id IN \((SELECT \w+ FROM \w+ WHERE .+)\)( ORDER BY \w+)?$`)
	nullableApplicabilityRe = regexp.MustCompile(`(?i)\((\w+) IS NULL OR (\w+)\s*=\s*(\?|\$\d+|SQLCARG_\w+)\)`)
)
