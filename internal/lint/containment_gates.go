package lint

import (
	"fmt"
	"regexp"
	"sort"
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

// gateSites returns, per qualifier, where each match of the patterns sits.
func gateSites(res []*regexp.Regexp, scopes *sqlScopes, text string) map[string][]gateSite {
	out := map[string][]gateSite{}
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			qualifier := ""
			if m[2] >= 0 {
				qualifier = text[m[2]:m[3]]
			}
			out[qualifier] = append(out[qualifier], scopes.site(text, m[0]))
		}
	}
	return out
}

// gateText is the form the gate patterns run on: comments removed, quoted
// identifiers and non-gate string literals masked, lower-cased, whitespace
// collapsed.
func gateText(q Query) string {
	src := q.Source
	if src == "" {
		src = q.SQL
	}
	return normalizeSpace(maskSQL(src))
}

// covered reports whether a privacy predicate has a containment predicate of
// its qualifier in the same query scope that every returned row had to satisfy.
func covered(privacy gateSite, containment []gateSite) bool {
	for _, c := range containment {
		if c.required && c.scope == privacy.scope {
			return true
		}
	}
	return false
}

// CheckContainmentGates requires every privacy-active predicate in the query
// files (`privacy_state = 'active'`, its reversed and IN forms, and the
// `<> 'restricted'` and `!=` forms) to be paired with a containment-none
// predicate on the same qualifier (principal alias) in the same query scope
// (the same subquery, and the same UNION operand), so a new authority or
// sign-in site cannot ship with only half of the effective-authority rule.
// Only a required conjunct counts: not one in a commented-out, quoted or
// string-literal position, one side of an OR, a NOT-negated group, a CASE
// expression or a SELECT-list expression. See sqlscope.go for what is read and
// for the residual limits; the behavioural isolation tests are the backstop.
func CheckContainmentGates(queries []Query, exempt map[string]string) []string {
	var findings []string
	seen := map[string]bool{}
	for _, q := range queries {
		text := gateText(q)
		scopes := scanSQLScopes(text)
		privacy := gateSites(privacyGateRes, scopes, text)
		if len(privacy) == 0 {
			continue
		}
		seen[q.Name] = true
		contained := gateSites(containmentGateRes, scopes, text)
		missing := false
		for qualifier, sites := range privacy {
			for _, p := range sites {
				if !covered(p, contained[qualifier]) {
					missing = true
				}
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
