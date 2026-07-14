package media

import (
	"os"
	"path/filepath"
	"testing"

	"toonflow/skill"
)

func TestMain(m *testing.M) {
	loadTestSkills()
	os.Exit(m.Run())
}

func loadTestSkills() {
	wd, _ := os.Getwd()
	for _, c := range []string{"skills", "../skills", "../../skills"} {
		p := filepath.Join(wd, c)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			mgr := skill.NewManager(p)
			_ = mgr.Load()
			skill.SetDefault(mgr)
			resetVideoI2VSkillCache()
			return
		}
	}
}
