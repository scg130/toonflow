package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSectionText(t *testing.T) {
	md := "# root\n\n## alpha\none\ntwo\n\n## beta\nthree\n"
	got := SectionText(md, "alpha")
	if got != "one\ntwo" {
		t.Fatalf("alpha=%q", got)
	}
	if SectionText(md, "beta") != "three" {
		t.Fatalf("beta wrong")
	}
}

func TestFileAndFormat(t *testing.T) {
	dir := findRepoSkills(t)
	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	SetDefault(m)
	t.Cleanup(func() { SetDefault(nil) })

	body := m.File("prompts/storyboard_parse.md")
	if body == "" {
		t.Fatal("storyboard_parse.md missing")
	}
	got := m.Format("prompts/storyboard_parse.md", 20, 18, 25)
	if got == "" || got == body {
		// body has %d — Format should substitute
	}
	if !containsFmt(got, "20") {
		t.Fatalf("format did not inject shot count: %s", trunc(got, 120))
	}
	neg := m.Section("prompts/video_i2v.md", "negative")
	if neg == "" {
		t.Fatal("video_i2v negative section missing")
	}
	lines := SectionLines(m.File("prompts/video_i2v.md"), "style_tags")
	if len(lines) < 3 {
		t.Fatalf("style_tags too few: %v", lines)
	}
}

func findRepoSkills(t *testing.T) string {
	t.Helper()
	candidates := []string{"skills", "../skills", "../../skills"}
	wd, _ := os.Getwd()
	for _, c := range candidates {
		p := filepath.Join(wd, c)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	t.Fatal("skills dir not found from ", wd)
	return ""
}

func containsFmt(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
