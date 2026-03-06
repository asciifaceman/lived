package config

import "testing"

func TestLoadFromEnv_MMOFeatureFlags(t *testing.T) {
	t.Setenv("LIVED_MMO_AUTH_ENABLED", "true")
	t.Setenv("LIVED_MMO_REALM_SCOPING_ENABLED", "false")
	t.Setenv("LIVED_MMO_CHAT_ENABLED", "false")
	t.Setenv("LIVED_MMO_ADMIN_ENABLED", "true")
	t.Setenv("LIVED_MMO_OTEL_ENABLED", "true")
	t.Setenv("LIVED_ENV", "production")
	t.Setenv("LIVED_OTEL_SERVICE_NAME", "lived-test")
	t.Setenv("LIVED_OTEL_ENDPOINT", "otel-collector:4317")
	t.Setenv("LIVED_OTEL_INSECURE", "false")
	t.Setenv("LIVED_OTEL_SAMPLE_RATIO", "0.25")
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
	if cfg.OTELServiceName != "lived-test" {
		t.Fatalf("expected OTel service name lived-test, got %q", cfg.OTELServiceName)
	}
	if cfg.OTELEndpoint != "otel-collector:4317" {
		t.Fatalf("expected OTel endpoint otel-collector:4317, got %q", cfg.OTELEndpoint)
	}
	if cfg.OTELInsecure {
		t.Fatalf("expected OTel insecure false")
	}
	if cfg.OTELSampleRatio != 0.25 {
		t.Fatalf("expected OTel sample ratio 0.25, got %v", cfg.OTELSampleRatio)
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
	if cfg.OTELServiceName != "lived" {
		t.Fatalf("expected default OTel service name lived, got %q", cfg.OTELServiceName)
	}
	if cfg.OTELEndpoint != "localhost:4317" {
		t.Fatalf("expected default OTel endpoint localhost:4317, got %q", cfg.OTELEndpoint)
	}
	if !cfg.OTELInsecure {
		t.Fatalf("expected default OTel insecure true")
	}
	if cfg.OTELSampleRatio != 1.0 {
		t.Fatalf("expected default OTel sample ratio 1.0, got %v", cfg.OTELSampleRatio)
	}
	if cfg.StreamMaxConnsPerAccount != 5 {
		t.Fatalf("expected default stream max per account 5, got %d", cfg.StreamMaxConnsPerAccount)
	}
	if cfg.StreamMaxConnsPerSession != 2 {
		t.Fatalf("expected default stream max per session 2, got %d", cfg.StreamMaxConnsPerSession)
	}
}
