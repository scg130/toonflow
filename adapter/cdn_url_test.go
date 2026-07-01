package adapter

import "testing"

func TestIsCDNImageURL(t *testing.T) {
	ok := []string{
		"https://platform-outputs.agnes-ai.space/images/t2i/abc.png",
		"http://cdn.example.com/a.png",
	}
	for _, u := range ok {
		if !IsCDNImageURL(u) {
			t.Fatalf("IsCDNImageURL(%q) = false, want true", u)
		}
	}
	bad := []string{
		"",
		"data:image/png;base64,abc",
		"/output/task/shot_001.png",
		"iVBORw0KGgo",
	}
	for _, u := range bad {
		if IsCDNImageURL(u) {
			t.Fatalf("IsCDNImageURL(%q) = true, want false", u)
		}
	}
}
