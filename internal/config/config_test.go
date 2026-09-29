package config

import "testing"

func TestShortAdminTokenRequiresExplicitOverride(t *testing.T) {
	t.Setenv("BLOG_ADMIN_TOKEN", "iknowit")
	t.Setenv("BLOG_DATABASE_URL", "postgres://localhost/blog")
	t.Setenv("BLOG_ALLOW_SHORT_ADMIN_TOKEN", "false")
	if _, err := Load(); err == nil {
		t.Fatal("short token accepted without override")
	}
	t.Setenv("BLOG_ALLOW_SHORT_ADMIN_TOKEN", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminToken != "iknowit" || !cfg.AllowShortAdminToken {
		t.Fatalf("unexpected token config: %+v", cfg)
	}
}
