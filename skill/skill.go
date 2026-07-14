package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager loads and caches skill markdown files for prompt composition.
type Manager struct {
	skillsDir string
	cache     map[string]string // category -> concatenated markdown
	files     map[string]string // relative path (slash) -> file body
	mu        sync.RWMutex
}

var (
	defaultMu  sync.RWMutex
	defaultMgr *Manager
)

// SetDefault registers the process-wide skill manager (call from main after Load).
func SetDefault(m *Manager) {
	defaultMu.Lock()
	defaultMgr = m
	defaultMu.Unlock()
}

// Default returns the process-wide skill manager (may be nil in tests).
func Default() *Manager {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultMgr
}

// NewManager creates a new skill manager.
func NewManager(skillsDir string) *Manager {
	return &Manager{
		skillsDir: skillsDir,
		cache:     make(map[string]string),
		files:     make(map[string]string),
	}
}

// Load reads all skill markdown files from the skills directory.
func (m *Manager) Load() error {
	if m == nil {
		return nil
	}
	if _, err := os.Stat(m.skillsDir); os.IsNotExist(err) {
		return nil
	}

	m.mu.Lock()
	m.cache = make(map[string]string)
	m.files = make(map[string]string)
	m.mu.Unlock()

	return filepath.Walk(m.skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		rel, err := filepath.Rel(m.skillsDir, path)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill %s: %w", path, err)
		}
		body := string(content)

		parts := strings.SplitN(relSlash, "/", 2)
		category := parts[0]

		m.mu.Lock()
		m.files[relSlash] = body
		// prompts/ is loaded by File() only — keep category dumps free of huge templates.
		if category != "prompts" {
			m.cache[category] += "\n---\n" + body
		}
		m.mu.Unlock()
		return nil
	})
}

// Get returns the concatenated prompt for a category (e.g. story_skills).
func (m *Manager) Get(category string) string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache[category]
}

// File returns one skill markdown body by relative path (slash-separated).
// Example: "prompts/storyboard_parse.md"
func (m *Manager) File(rel string) string {
	if m == nil {
		return ""
	}
	rel = filepath.ToSlash(strings.TrimPrefix(rel, "/"))
	m.mu.RLock()
	body, ok := m.files[rel]
	m.mu.RUnlock()
	if ok {
		return body
	}
	// Lazy read if Load missed (or called before Load).
	path := filepath.Join(m.skillsDir, filepath.FromSlash(rel))
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	body = string(b)
	m.mu.Lock()
	m.files[rel] = body
	m.mu.Unlock()
	return body
}

// Format is like fmt.Sprintf over Manager.File(rel).
func (m *Manager) Format(rel string, args ...any) string {
	body := strings.TrimSpace(m.File(rel))
	if body == "" || len(args) == 0 {
		return body
	}
	return fmt.Sprintf(body, args...)
}

// Section extracts the body under "## heading" until the next "## " heading.
func (m *Manager) Section(rel, heading string) string {
	return SectionText(m.File(rel), heading)
}

// GetAll returns all category prompts concatenated.
func (m *Manager) GetAll() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var parts []string
	for _, content := range m.cache {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n===\n")
}

// GetArtPrompt loads an art style's prefix file and optionally a specific file.
func (m *Manager) GetArtPrompt(styleName, fileName string) string {
	if m == nil {
		return ""
	}
	dir := filepath.Join(m.skillsDir, "art_skills", styleName)
	var result strings.Builder

	if content, err := os.ReadFile(filepath.Join(dir, "prefix.md")); err == nil {
		result.Write(content)
	}

	if fileName != "" {
		if content, err := os.ReadFile(filepath.Join(dir, fileName+".md")); err == nil {
			result.WriteString("\n---\n")
			result.Write(content)
		}
	}

	return result.String()
}

// --- package-level helpers (use SetDefault) ---

// File reads a skill file from the default manager.
func File(rel string) string {
	if m := Default(); m != nil {
		return m.File(rel)
	}
	return ""
}

// Format formats a skill file from the default manager.
func Format(rel string, args ...any) string {
	if m := Default(); m != nil {
		return m.Format(rel, args...)
	}
	return ""
}

// Section reads a ## heading section from a skill file via the default manager.
func Section(rel, heading string) string {
	if m := Default(); m != nil {
		return m.Section(rel, heading)
	}
	return ""
}

// SectionText extracts "## heading" body from markdown text.
func SectionText(content, heading string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	heading = strings.TrimSpace(heading)
	if content == "" || heading == "" {
		return ""
	}
	needle := "## " + heading
	idx := strings.Index(content, needle)
	if idx < 0 {
		// allow exact match without requiring space variants
		needle = "##" + heading
		idx = strings.Index(content, needle)
		if idx < 0 {
			return ""
		}
	}
	rest := content[idx+len(needle):]
	rest = strings.TrimPrefix(rest, "\n")
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}

// SectionLines returns non-empty, non-comment lines of a section.
func SectionLines(content, heading string) []string {
	body := SectionText(content, heading)
	if body == "" {
		return nil
	}
	raw := strings.Split(body, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		// bullet "- text"
		if strings.HasPrefix(line, "- ") {
			line = strings.TrimSpace(line[2:])
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
