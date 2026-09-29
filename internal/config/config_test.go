package config

import "testing"

func TestShortAdminTokenRequiresExplicitOverride(t *testing.T) {
	t.Setenv("BLOG_LEGACY_ADMIN", "true")
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

func TestWebsiteRejectsInsecurePublicOrigin(t *testing.T) {
	t.Setenv("BLOG_DATABASE_URL", "postgres://localhost/blog")
	t.Setenv("BLOG_LEGACY_ADMIN", "false")
	t.Setenv("BLOG_SITE_URL", "http://blog.example.com")
	if _, e := Load(); e == nil {
		t.Fatal("insecure public cookies accepted")
	}
	t.Setenv("BLOG_SITE_URL", "https://blog.example.com")
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
}

func TestSMTPTransportConfiguration(t *testing.T) {
	t.Setenv("BLOG_DATABASE_URL", "postgres://localhost/blog")
	t.Setenv("BLOG_LEGACY_ADMIN", "false")
	t.Setenv("BLOG_SITE_URL", "https://blog.example.test")
	for _, tc := range []struct {
		address, mode string
		valid         bool
	}{
		{"127.0.0.1:25", "local", true},
		{"[::1]:25", "local", true},
		{"smtp.example.test:587", "starttls", true},
		{"smtp.example.test:465", "tls", true},
		{"smtp.example.test:25", "local", false},
		{"smtp.example.test", "starttls", false},
		{"smtp.example.test:25", "plaintext", false},
	} {
		t.Run(tc.address+tc.mode, func(t *testing.T) {
			t.Setenv("BLOG_SMTP_ADDR", tc.address)
			t.Setenv("BLOG_SMTP_MODE", tc.mode)
			_, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t: %v", tc.valid, err)
			}
		})
	}
}
