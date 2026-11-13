// Package patterns holds every status regex. Nothing else in perch has regexes.
package patterns

import "regexp"

var (
	// NeedsInput matches a permission prompt or a y/n question waiting on the user.
	NeedsInput = []*regexp.Regexp{
		regexp.MustCompile(`Do you want to (proceed|make this edit|create|allow)`),
		regexp.MustCompile(`(?m)^\s*[❯>]\s*1\.\s+Yes\b`),
		regexp.MustCompile(`(?i)\(y/n\)|\[y/n\]|\(yes/no\)`),
		regexp.MustCompile(`(?i)Enter to confirm . Esc to cancel`),
	}

	// Running matches the spinner footer Claude Code shows while it works.
	Running = []*regexp.Regexp{
		regexp.MustCompile(`(?i)esc to interrupt`),
	}
)

// Any reports whether s matches at least one of res.
func Any(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
