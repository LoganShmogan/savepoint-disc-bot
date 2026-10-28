package config

import "testing"

func TestMatch(t *testing.T) {
	p := &Project{Docs: []string{"docs/**", "README.md"}, DevFiles: []string{".env.example", "prisma/*.prisma"}}
	for path, want := range map[string]string{
		"docs/a.md": KindDocs, "docs/deep/b.png": KindDocs, "README.md": KindDocs,
		".env.example": KindDev, "prisma/schema.prisma": KindDev,
	} {
		if got, ok := p.Match(path); !ok || got != want {
			t.Errorf("Match(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	if _, ok := p.Match("src/main.go"); ok {
		t.Error("src/main.go should not match")
	}
}
