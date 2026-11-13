package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ContainmentGateExemptions names the queries that may test the privacy gate
// without the containment condition, each with its reason. Privacy's own
// owner queries (privacy.sql) never spell a privacy-active test, so they need
// no entry. A stale entry (the query is gone, or now carries the gate) is
// itself a finding.
var ContainmentGateExemptions = map[string]string{
	"GetAccountForRecovery":                       "the ONE recovery lookup: drops only containment, so reset and establishment reach a contained account",
	"RecoveryListGrantsBeforeSelfConfig":          "historical-recovery projection for source schemas that predate containment",
	"RecoveryListGrantsBeforeAccess":              "historical-recovery projection for source schemas that predate containment",
	"RecoveryListGrantsBeforeContainment":         "historical-recovery projection for source schemas that predate containment",
	"RecoveryListRulesBeforeContainment":          "historical-recovery projection for source schemas that predate containment",
	"RecoveryPersonalOwnerHoldsBeforeContainment": "historical-recovery projection for source schemas that predate containment",
}

// Every spelling of "the privacy gate lets this principal through", and of the
// containment condition that must accompany it. Group 1 of each is the
// qualifier (principal alias), so a gate on one alias cannot cover another.
// Matching runs on lower-cased, comment-free, whitespace-collapsed SQL.
const optionalQualifier = `(?:\b(\w+)\.)?`

var (
	privacyGateRes = compileAll(
		optionalQualifier+`\bprivacy_state\s*=\s*'active'`,
		`'active'\s*=\s*`+optionalQualifier+`\bprivacy_state\b`,
		optionalQualifier+`\bprivacy_state\s+in\s*\(\s*'active'\s*\)`,
		optionalQualifier+`\bprivacy_state\s*(?:<>|!=)\s*'restricted'`,
		`'restricted'\s*(?:<>|!=)\s*`+optionalQualifier+`\bprivacy_state\b`,
		optionalQualifier+`\bprivacy_state\s+not\s+in\s*\(\s*'restricted'\s*\)`,
	)
	containmentGateRes = compileAll(
		optionalQualifier+`\bcontainment_state\s*=\s*'none'`,
		`'none'\s*=\s*`+optionalQualifier+`\bcontainment_state\b`,
		optionalQualifier+`\bcontainment_state\s+in\s*\(\s*'none'\s*\)`,
		optionalQualifier+`\bcontainment_state\s*(?:<>|!=)\s*'contained'`,
		`'contained'\s*(?:<>|!=)\s*`+optionalQualifier+`\bcontainment_state\b`,
		optionalQualifier+`\bcontainment_state\s+not\s+in\s*\(\s*'contained'\s*\)`,
	)
)

func compileAll(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(p)
	}
	return out
}

func gateCounts(res []*regexp.Regexp, sql string) map[string]int {
	out := map[string]int{}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(sql, -1) {
			out[m[1]]++
		}
	}
	return out
}

// stripSQLComments removes `--` line comments and (nested) `/* */` block
// comments, leaving string literals untouched, so a comment cannot satisfy or
// hide a predicate.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); {
		switch {
		case sql[i] == '\'':
			j := i + 1
			for j < len(sql) {
				if sql[j] == '\'' {
					if j+1 < len(sql) && sql[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j < len(sql) {
				j++
			}
			b.WriteString(sql[i:j])
			i = j
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			depth := 0
			for i < len(sql) {
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
			b.WriteByte(sql[i])
			i++
		}
	}
	return b.String()
}

// gateText is the form the gate patterns run on.
func gateText(q Query) string {
	src := q.Source
	if src == "" {
		src = q.SQL
	}
	return strings.ToLower(normalizeSpace(stripSQLComments(src)))
}

// CheckContainmentGates requires every privacy-active predicate in the query
// files (`privacy_state = 'active'`, its reversed and IN forms, and the
// `<> 'restricted'` and `!=` forms) to be paired with a containment-none
// predicate on the same qualifier (principal alias), so a new authority or
// sign-in site cannot ship with only half of the effective-authority rule.
// Comments are stripped first: a commented-out condition does not count.
func CheckContainmentGates(queries []Query, exempt map[string]string) []string {
	var findings []string
	seen := map[string]bool{}
	for _, q := range queries {
		text := gateText(q)
		privacy := gateCounts(privacyGateRes, text)
		if len(privacy) == 0 {
			continue
		}
		seen[q.Name] = true
		contained := gateCounts(containmentGateRes, text)
		missing := false
		for qualifier, n := range privacy {
			if contained[qualifier] < n {
				missing = true
			}
		}
		_, exempted := exempt[q.Name]
		switch {
		case missing && !exempted:
			findings = append(findings, fmt.Sprintf("containment gate: query %s tests the privacy gate without the matching containment_state = 'none'", q.Name))
		case !missing && exempted:
			findings = append(findings, fmt.Sprintf("containment gate: query %s is exempted but already carries the containment condition (stale exemption)", q.Name))
		}
	}
	names := make([]string, 0, len(exempt))
	for name := range exempt {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !seen[name] {
			findings = append(findings, fmt.Sprintf("containment gate: exempted query %s has no privacy_state = 'active' predicate (stale exemption)", name))
		}
	}
	return findings
}
