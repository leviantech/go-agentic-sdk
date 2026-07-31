package guardrails

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// ContentFilter blocks input/output containing forbidden patterns.
// Patterns can be plain substrings or regexes (prefix "re:").
type ContentFilter struct {
	substrings []string
	patterns   []*regexp.Regexp
}

func NewContentFilter(blocked ...string) *ContentFilter {
	f := &ContentFilter{}
	for _, b := range blocked {
		if strings.HasPrefix(b, "re:") {
			if re, err := regexp.Compile(strings.TrimPrefix(b, "re:")); err == nil {
				f.patterns = append(f.patterns, re)
			}
			continue
		}
		f.substrings = append(f.substrings, strings.ToLower(b))
	}
	return f
}

// Match returns the first matching pattern, or "".
func (f *ContentFilter) Match(s string) string {
	lower := strings.ToLower(s)
	for _, sub := range f.substrings {
		if strings.Contains(lower, sub) {
			return sub
		}
	}
	for _, re := range f.patterns {
		if re.MatchString(s) {
			return re.String()
		}
	}
	return ""
}

func (f *ContentFilter) ValidateInput(_ context.Context, input string) error {
	if m := f.Match(input); m != "" {
		return fmt.Errorf("input contains forbidden content: %q", m)
	}
	return nil
}

func (f *ContentFilter) ValidateOutput(_ context.Context, output string) error {
	if m := f.Match(output); m != "" {
		return fmt.Errorf("output contains forbidden content: %q", m)
	}
	return nil
}
