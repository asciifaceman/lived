package config

import "testing"

func TestLoadFromEnv_MMOFeatureFlags(t *testing.T) {
	t.Setenv("LIVED_MMO_AUTH_ENABLED", "true")
	t.Setenv("LIVED_MMO_REALM_SCOPING_ENABLED", "false")
	t.Setenv("LIVED_MMO_CHAT_ENABLED", "false")
	t.Setenv("LIVED_MMO_ADMIN_ENABLED", "true")
	t.Setenv("LIVED_MMO_OTEL_ENABLED", "true")
	t.Setenv("LIVED_MMO_JWT_SECRET", "secret")
	t.Setenv("LIVED_RATE_LIMIT_ENABLED", " TRUE ")
	t.Setenv("LIVED_IDEMPOTENCY_ENABLED", " On ")
	t.Setenv("LIVED_STREAM_MAX_CONNS_PER_ACCOUNT", "7")
	t.Setenv("LIVED_STREAM_MAX_CONNS_PER_SESSION", "3")

	cfg := LoadFromEnv()

	if !cfg.MMOAuthEnabled {
		t.Fatalf("expected MMO auth enabled")
	}
	if cfg.MMORealmScopingEnabled {
		t.Fatalf("expected MMO realm scoping disabled")
	}
	if cfg.MMOChatEnabled {
		t.Fatalf("expected MMO chat disabled")
	}
	if !cfg.MMOAdminEnabled {
		t.Fatalf("expected MMO admin enabled")
	}
	if !cfg.MMOOTelEnabled {
		t.Fatalf("expected MMO OTel enabled")
	}
	if !cfg.RateLimitEnabled {
		t.Fatalf("expected rate limiting enabled from whitespace/case tolerant bool")
	}
	if !cfg.IdempotencyEnabled {
		t.Fatalf("expected idempotency enabled from whitespace/case tolerant bool")
	}
	if cfg.StreamMaxConnsPerAccount != 7 {
		t.Fatalf("expected stream max per account 7, got %d", cfg.StreamMaxConnsPerAccount)
	}
	if cfg.StreamMaxConnsPerSession != 3 {
		t.Fatalf("expected stream max per session 3, got %d", cfg.StreamMaxConnsPerSession)
	}
}

func TestLoadFromEnv_DefaultFeatureFlagValues(t *testing.T) {
	t.Setenv("LIVED_MMO_AUTH_ENABLED", "")
	t.Setenv("LIVED_MMO_REALM_SCOPING_ENABLED", "")
	t.Setenv("LIVED_MMO_CHAT_ENABLED", "")
	t.Setenv("LIVED_MMO_ADMIN_ENABLED", "")
	t.Setenv("LIVED_MMO_OTEL_ENABLED", "")

	cfg := LoadFromEnv()

	if cfg.MMOAuthEnabled {
		t.Fatalf("expected MMO auth disabled by default")
	}
	if !cfg.MMORealmScopingEnabled {
		t.Fatalf("expected MMO realm scoping enabled by default")
	}
	if !cfg.MMOChatEnabled {
		t.Fatalf("expected MMO chat enabled by default")
	}
	if !cfg.MMOAdminEnabled {
		t.Fatalf("expected MMO admin enabled by default")
	}
	if cfg.MMOOTelEnabled {
		t.Fatalf("expected MMO OTel disabled by default")
	}
	if cfg.StreamMaxConnsPerAccount != 5 {
		t.Fatalf("expected default stream max per account 5, got %d", cfg.StreamMaxConnsPerAccount)
	}
	if cfg.StreamMaxConnsPerSession != 2 {
		t.Fatalf("expected default stream max per session 2, got %d", cfg.StreamMaxConnsPerSession)
	}
}
