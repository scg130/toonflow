package asset

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	speakerBracketLineRE = regexp.MustCompile(`^\s*【([\p{Han}]{1,8})】`)
	speakerColonLineRE   = regexp.MustCompile(`^([\p{Han}]{1,8})\s*[：:]`)
	speakerPipeLineRE    = regexp.MustCompile(`^([\p{Han}]{1,6})\s*\|`)
)

// HasCJK reports whether s contains a CJK (Han) rune.
func HasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// ExtractSpeakerNamesFromScript collects ordered unique Chinese speaker names from script text.
func ExtractSpeakerNamesFromScript(script string) []string {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	seen := map[string]bool{}
	var order []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] || !HasCJK(n) {
			return
		}
		if len([]rune(n)) > 8 {
			return
		}
		seen[n] = true
		order = append(order, n)
	}
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, re := range []*regexp.Regexp{speakerBracketLineRE, speakerColonLineRE, speakerPipeLineRE} {
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				add(m[1])
				break
			}
		}
	}
	return order
}

// AssignChineseRoleNames rewrites ASCII-only role names using Chinese names found in the script.
func AssignChineseRoleNames(items []extractAssetItem, script string) {
	speakers := ExtractSpeakerNamesFromScript(script)
	if len(speakers) == 0 {
		return
	}
	used := map[string]bool{}
	var asciiIdx []int
	for i, it := range items {
		if it.Type != "role" {
			continue
		}
		if HasCJK(it.Name) {
			used[it.Name] = true
			continue
		}
		asciiIdx = append(asciiIdx, i)
	}
	var available []string
	for _, sp := range speakers {
		if !used[sp] {
			available = append(available, sp)
		}
	}
	for j, idx := range asciiIdx {
		if j >= len(available) {
			break
		}
		items[idx].Name = available[j]
	}
}
