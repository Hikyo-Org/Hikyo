// Package parameters implements bounded, single-pass config substitution.
// Parameters are public configuration, never secret inputs or expressions.
package parameters

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxContractBytes bounds immutable schema metadata per publication.
	MaxContractBytes = 256 << 10
	MaxCount         = 32
	MaxValueBytes    = 256
	MaxPatternBytes  = 512
)

var namePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Contract is immutable snapshot metadata. Schemas records the declarations of
// templated config keys, so historical delivery never consults the live schema.
type Contract struct {
	Version      int               `json:"version,omitempty"`
	Declarations map[string]string `json:"declarations,omitempty"`
	Schemas      map[string]string `json:"schemas,omitempty"`
}

func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("parameter name %q must match %s", name, namePattern.String())
	}
	return nil
}

func CheckDeclaration(name, pattern string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	if len(pattern) == 0 || len(pattern) > MaxPatternBytes {
		return fmt.Errorf("parameter %s pattern must contain 1 to %d bytes", name, MaxPatternBytes)
	}
	if strings.ContainsRune(pattern, 0) {
		return fmt.Errorf("parameter %s pattern must not contain a NUL character", name)
	}
	if _, err := compiledPattern(pattern); err != nil {
		return fmt.Errorf("parameter %s has an invalid pattern: %w", name, err)
	}
	return nil
}

func CheckSupplied(values map[string]string) error {
	if len(values) > MaxCount {
		return fmt.Errorf("at most %d parameters are allowed", MaxCount)
	}
	for name, value := range values {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("invalid parameter name %q", name)
		}
		if len(value) > MaxValueBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("parameter %s must be valid text without control characters, at most %d bytes", name, MaxValueBytes)
		}
	}
	return nil
}

func Validate(declarations, supplied map[string]string) error {
	if err := CheckSupplied(supplied); err != nil {
		return err
	}
	for _, name := range names(supplied) {
		if _, ok := declarations[name]; !ok {
			return fmt.Errorf("undeclared parameter %s", name)
		}
	}
	for _, name := range names(declarations) {
		pattern := declarations[name]
		if err := CheckDeclaration(name, pattern); err != nil {
			return err
		}
		value, ok := supplied[name]
		if !ok {
			return fmt.Errorf("required parameter %s is missing", name)
		}
		re, err := compiledPattern(pattern)
		if err != nil {
			return err
		}
		if !re.MatchString(value) {
			return fmt.Errorf("parameter %s does not match its validation pattern", name)
		}
	}
	return nil
}

// A bounded FIFO retains compiled RE2 patterns across deliveries. Invalid
// patterns are not cached, and the lock prevents concurrent duplicate compiles.
const maxCachedPatterns = 128

var patternCache = struct {
	sync.Mutex
	entries map[string]*regexp.Regexp
	order   []string
}{entries: make(map[string]*regexp.Regexp)}

func compiledPattern(pattern string) (*regexp.Regexp, error) {
	patternCache.Lock()
	defer patternCache.Unlock()
	if re := patternCache.entries[pattern]; re != nil {
		return re, nil
	}
	re, err := regexp.Compile("\\A(?:" + pattern + ")\\z")
	if err != nil {
		return nil, err
	}
	if len(patternCache.order) == maxCachedPatterns {
		delete(patternCache.entries, patternCache.order[0])
		patternCache.order = patternCache.order[1:]
	}
	patternCache.entries[pattern] = re
	patternCache.order = append(patternCache.order, pattern)
	return re, nil
}

// References accepts ${NAME}; $${ escapes a literal ${ without a closing brace.
func References(value string) ([]string, error) {
	var refs []string
	_, err := transform(value, func(name string) (string, error) {
		refs = append(refs, name)
		return "", nil
	}, -1)
	return refs, err
}

func CheckReferences(value string, declarations map[string]string) error {
	if len(declarations) == 0 {
		return nil
	}
	refs, err := References(value)
	if err != nil {
		return err
	}
	for _, name := range refs {
		if _, ok := declarations[name]; !ok {
			return fmt.Errorf("undeclared parameter %s", name)
		}
	}
	return nil
}

// Resolve substitutes once. Caller inputs are never scanned as new expressions.
func Resolve(value string, supplied map[string]string, maxBytes int) (string, error) {
	return transform(value, func(name string) (string, error) {
		replacement, ok := supplied[name]
		if !ok {
			return "", fmt.Errorf("required parameter %s is missing", name)
		}
		return replacement, nil
	}, maxBytes)
}

func transform(value string, replacement func(string) (string, error), maxBytes int) (string, error) {
	var out strings.Builder
	for len(value) > 0 {
		index := strings.Index(value, "${")
		if index < 0 {
			out.WriteString(value)
			break
		}
		if index > 0 && value[index-1] == '$' {
			out.WriteString(value[:index-1])
			out.WriteString("${")
			value = value[index+2:]
		} else {
			out.WriteString(value[:index])
			name, rest, closed := strings.Cut(value[index+2:], "}")
			if !closed || !namePattern.MatchString(name) {
				return "", fmt.Errorf("invalid parameter reference; expected ${NAME} or $${")
			}
			text, err := replacement(name)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
			value = rest
		}
		if maxBytes >= 0 && out.Len() > maxBytes {
			return "", fmt.Errorf("resolved config exceeds %d bytes", maxBytes)
		}
	}
	if maxBytes >= 0 && out.Len() > maxBytes {
		return "", fmt.Errorf("resolved config exceeds %d bytes", maxBytes)
	}
	return out.String(), nil
}

func Encode(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func names(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for name := range values {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
