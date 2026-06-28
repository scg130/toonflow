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
	cache     map[string]string
	mu        sync.RWMutex
}

// NewManager creates a new skill manager.
func NewManager(skillsDir string) *Manager {
	return &Manager{
		skillsDir: skillsDir,
		cache:     make(map[string]string),
	}
}

// Load reads all skill markdown files from the skills directory.
func (m *Manager) Load() error {
	if _, err := os.Stat(m.skillsDir); os.IsNotExist(err) {
		return nil
	}

	return filepath.Walk(m.skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		rel, _ := filepath.Rel(m.skillsDir, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		category := parts[0]

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read skill %s: %w", path, err)
		}

		m.mu.Lock()
		m.cache[category] += "\n---\n" + string(content)
		m.mu.Unlock()

		return nil
	})
}

// Get returns the concatenated prompt for a category.
func (m *Manager) Get(category string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache[category]
}

// GetAll returns all skill prompts concatenated.
func (m *Manager) GetAll() string {
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
