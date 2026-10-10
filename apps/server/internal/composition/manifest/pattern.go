package manifest

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

// CompilePattern implements the portable RE2 grammar in 04 §4.2 (MF-001).
// MatchString searches for a match; only explicit anchors require the whole string.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	invalid := errors.New("pattern не входит в переносимое подмножество")
	if !utf8.ValidString(pattern) || strings.ContainsRune(pattern, 0) || len(pattern) > 1024 {
		return nil, invalid
	}
	chars := []rune(pattern)
	if len(chars) > 500 {
		return nil, invalid
	}
	inClass := false
	classStart := -1
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if c == '\\' {
			i++
			if i == len(chars) {
				return nil, invalid
			}
			escaped := chars[i]
			alphanumeric := escaped >= 'a' && escaped <= 'z' || escaped >= 'A' && escaped <= 'Z' || escaped >= '0' && escaped <= '9'
			punctuation := escaped >= 33 && escaped <= 126 && !alphanumeric
			if !strings.ContainsRune("afnrtvdDsSwWbBAz", escaped) && !punctuation {
				return nil, invalid
			}
		} else if c == '[' {
			if inClass && i+1 < len(chars) && chars[i+1] == ':' {
				return nil, invalid
			}
			if !inClass {
				classStart = i
			}
			inClass = true
		} else if c == ']' && inClass {
			if i != classStart+1 && !(chars[classStart+1] == '^' && i == classStart+2) {
				inClass = false
			}
		} else if !inClass && c == '(' && i+1 < len(chars) && chars[i+1] == '?' && (i+2 == len(chars) || chars[i+2] != ':') {
			return nil, invalid
		}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, invalid
	}
	return re, nil
}
