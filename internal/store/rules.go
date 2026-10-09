package store

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Rule problems.
const (
	ProblemRequired = "required" // a required key would lose its value
	ProblemPattern  = "pattern"  // the value does not match the key's pattern
)

// RuleError is one value that breaks its key's rules. The message names the
// key and the rule, never the value.
type RuleError struct {
	Key     string
	Problem string
	Pattern string
}

func (e *RuleError) Error() string {
	if e.Problem == ProblemRequired {
		return fmt.Sprintf("%s is required: it must keep a value in every environment", e.Key)
	}
	return fmt.Sprintf("%s must match the pattern %s", e.Key, e.Pattern)
}

// RuleErrors collects the failures of a bulk write such as an import.
type RuleErrors struct{ Items []*RuleError }

func (e *RuleErrors) Error() string {
	parts := make([]string, 0, len(e.Items))
	for i, it := range e.Items {
		if i == 5 {
			parts = append(parts, fmt.Sprintf("and %d more", len(e.Items)-5))
			break
		}
		parts = append(parts, it.Error())
	}
	return strings.Join(parts, "; ")
}

var patterns sync.Map // pattern -> *regexp.Regexp

func compiled(pattern string) (*regexp.Regexp, error) {
	if re, ok := patterns.Load(pattern); ok {
		return re.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	patterns.Store(pattern, re)
	return re, nil
}

// ValidatePattern checks a pattern before it is saved.
func ValidatePattern(pattern string) error {
	if len(pattern) > 500 {
		return fmt.Errorf("the pattern is longer than 500 characters")
	}
	if _, err := compiled(pattern); err != nil {
		return fmt.Errorf("the pattern is not a valid regular expression")
	}
	return nil
}

// RuleProblem says which rule a write would break: value nil is a deletion.
// It returns "" when the write is allowed.
func (k *Key) RuleProblem(value *string) string {
	if value == nil {
		if k.Required {
			return ProblemRequired
		}
		return ""
	}
	if k.Pattern != "" {
		if re, err := compiled(k.Pattern); err == nil && !re.MatchString(*value) {
			return ProblemPattern
		}
	}
	return ""
}

var tagName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// NormalizeTags lower-cases, trims and de-duplicates tags and checks them.
func NormalizeTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if !tagName.MatchString(t) {
			return nil, fmt.Errorf("tags are up to 32 letters, digits, '-' or '_', starting with a letter or digit")
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 10 {
		return nil, fmt.Errorf("a key can have at most 10 tags")
	}
	return out, nil
}
