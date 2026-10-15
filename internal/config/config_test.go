package config

import "testing"

func TestCustomPath(t *testing.T) {
	t.Setenv("PAPERLESS_URL", "http://localhost:8000")
	t.Setenv("PAPERLESS_TOKEN", "tok")

	t.Setenv("PAPERLESS_CUSTOM_PATH", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.CustomPath != "" {
		t.Errorf("CustomPath = %q, want empty", c.CustomPath)
	}

	t.Setenv("PAPERLESS_CUSTOM_PATH", "  /api/other/  ")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.CustomPath != "/api/other/" {
		t.Errorf("CustomPath = %q, want %q", c.CustomPath, "/api/other/")
	}
}
