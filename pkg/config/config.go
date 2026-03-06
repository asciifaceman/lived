package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	SSLMode  string
	TimeZone string
}

func (cfg PostgresConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		cfg.Host,
		cfg.User,
		cfg.Password,
		cfg.DBName,
		cfg.Port,
		cfg.SSLMode,
		cfg.TimeZone,
	)
}

type Config struct {
	HTTPAddr                 string
	DatabaseURL              string
	Postgres                 PostgresConfig
	PostgresAdminDB          string
	AutoMigrate              bool
	TickInterval             time.Duration
	GameMinutesRate          float64
	FrontendDevProxyURL      string
	MMOAuthEnabled           bool
	MMORealmScopingEnabled   bool
	MMOChatEnabled           bool
	MMOAdminEnabled          bool
	MMOOTelEnabled           bool
	OTELServiceName          string
	OTELEndpoint             string
	OTELInsecure             bool
	OTELSampleRatio          float64
	MMOJWTIssuer             string
	MMOJWTSecret             string
	MMOAccessTokenTTL        time.Duration
	MMORefreshTokenTTL       time.Duration
	RateLimitEnabled         bool
	RateLimitWindow          time.Duration
	RateLimitAuthMax         int
	RateLimitChatMax         int
	RateLimitBehaviorMax     int
	RateLimitOnboardMax      int
	RateLimitIdentity        string
	IdempotencyEnabled       bool
	IdempotencyTTL           time.Duration
	StreamMaxConnsPerAccount int
	StreamMaxConnsPerSession int
	StreamAllowQueryAccessToken bool
	StreamAllowedOrigins        []string
}

func LoadFromEnv() Config {
	_ = godotenv.Load()

	httpAddr := os.Getenv("LIVED_HTTP_ADDR")
	if httpAddr == "" {
		httpAddr = ":8080"
	}

	postgresCfg := PostgresConfig{
		Host:     getEnvOrDefault("LIVED_POSTGRES_HOST", "localhost"),
		Port:     getEnvOrDefault("LIVED_POSTGRES_PORT", "5432"),
		User:     getEnvOrDefault("LIVED_POSTGRES_USER", "postgres"),
		Password: getEnvOrDefault("LIVED_POSTGRES_PASSWORD", "postgres"),
		DBName:   getEnvOrDefault("LIVED_POSTGRES_DBNAME", "lived"),
		SSLMode:  getEnvOrDefault("LIVED_POSTGRES_SSLMODE", "disable"),
		TimeZone: getEnvOrDefault("LIVED_POSTGRES_TIMEZONE", "UTC"),
	}

	postgresAdminDB := getEnvOrDefault("LIVED_POSTGRES_ADMIN_DB", "postgres")
	tickInterval := getDurationOrDefault("LIVED_GAME_TICK_INTERVAL", time.Second)
	gameMinutesRate := getFloatOrDefault("LIVED_GAME_MINUTES_PER_REAL_MINUTE", 60)
	frontendDevProxyURL := getEnvOrDefault("LIVED_WEB_DEV_PROXY_URL", "")
	mmoAuthEnabled := getBoolOrDefault("LIVED_MMO_AUTH_ENABLED", false)
	mmoRealmScopingEnabled := getBoolOrDefault("LIVED_MMO_REALM_SCOPING_ENABLED", true)
	mmoChatEnabled := getBoolOrDefault("LIVED_MMO_CHAT_ENABLED", true)
	mmoAdminEnabled := getBoolOrDefault("LIVED_MMO_ADMIN_ENABLED", true)
	mmoOTelEnabled := getBoolOrDefault("LIVED_MMO_OTEL_ENABLED", false)
	otelServiceName := getEnvOrDefault("LIVED_OTEL_SERVICE_NAME", "lived")
	otelEndpoint := getEnvOrDefault("LIVED_OTEL_ENDPOINT", "localhost:4317")
	otelInsecure := getBoolOrDefault("LIVED_OTEL_INSECURE", true)
	otelSampleRatio := getOTELSampleRatio("LIVED_OTEL_SAMPLE_RATIO", getDefaultOTELSampleRatio(getEnvOrDefault("LIVED_ENV", "development")))
	mmoJWTIssuer := getEnvOrDefault("LIVED_MMO_JWT_ISSUER", "lived")
	mmoJWTSecret := getEnvOrDefault("LIVED_MMO_JWT_SECRET", "")
	if mmoAuthEnabled && mmoJWTSecret == "" {
		mmoJWTSecret = "dev-insecure-change-me"
	}
	mmoAccessTokenTTL := getDurationOrDefault("LIVED_MMO_ACCESS_TOKEN_TTL", 15*time.Minute)
	mmoRefreshTokenTTL := getDurationOrDefault("LIVED_MMO_REFRESH_TOKEN_TTL", 30*24*time.Hour)
	rateLimitEnabled := getBoolOrDefault("LIVED_RATE_LIMIT_ENABLED", false)
	rateLimitWindow := getDurationOrDefault("LIVED_RATE_LIMIT_WINDOW", time.Minute)
	rateLimitAuthMax := getIntOrDefault("LIVED_RATE_LIMIT_AUTH_MAX", 20)
	rateLimitChatMax := getIntOrDefault("LIVED_RATE_LIMIT_CHAT_MAX", 30)
	rateLimitBehaviorMax := getIntOrDefault("LIVED_RATE_LIMIT_BEHAVIOR_MAX", 30)
	rateLimitOnboardMax := getIntOrDefault("LIVED_RATE_LIMIT_ONBOARD_MAX", 10)
	rateLimitIdentity := getRateLimitIdentityOrDefault("LIVED_RATE_LIMIT_IDENTITY", "ip")
	idempotencyEnabled := getBoolOrDefault("LIVED_IDEMPOTENCY_ENABLED", false)
	idempotencyTTL := getDurationOrDefault("LIVED_IDEMPOTENCY_TTL", 10*time.Minute)
	streamMaxConnsPerAccount := getIntOrDefault("LIVED_STREAM_MAX_CONNS_PER_ACCOUNT", 5)
	streamMaxConnsPerSession := getIntOrDefault("LIVED_STREAM_MAX_CONNS_PER_SESSION", 2)
	streamAllowQueryAccessToken := getBoolOrDefault("LIVED_STREAM_QUERY_ACCESS_TOKEN_ENABLED", false)
	streamAllowedOrigins := getCSVOrDefault("LIVED_STREAM_ALLOWED_ORIGINS", nil)

	databaseURL := os.Getenv("LIVED_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = postgresCfg.DSN()
	}

	autoMigrate := os.Getenv("LIVED_AUTO_MIGRATE")
	if autoMigrate == "false" || autoMigrate == "0" {
		return Config{
			HTTPAddr:                 httpAddr,
			DatabaseURL:              databaseURL,
			Postgres:                 postgresCfg,
			PostgresAdminDB:          postgresAdminDB,
			AutoMigrate:              false,
			TickInterval:             tickInterval,
			GameMinutesRate:          gameMinutesRate,
			FrontendDevProxyURL:      frontendDevProxyURL,
			MMOAuthEnabled:           mmoAuthEnabled,
			MMORealmScopingEnabled:   mmoRealmScopingEnabled,
			MMOChatEnabled:           mmoChatEnabled,
			MMOAdminEnabled:          mmoAdminEnabled,
			MMOOTelEnabled:           mmoOTelEnabled,
			OTELServiceName:          otelServiceName,
			OTELEndpoint:             otelEndpoint,
			OTELInsecure:             otelInsecure,
			OTELSampleRatio:          otelSampleRatio,
			MMOJWTIssuer:             mmoJWTIssuer,
			MMOJWTSecret:             mmoJWTSecret,
			MMOAccessTokenTTL:        mmoAccessTokenTTL,
			MMORefreshTokenTTL:       mmoRefreshTokenTTL,
			RateLimitEnabled:         rateLimitEnabled,
			RateLimitWindow:          rateLimitWindow,
			RateLimitAuthMax:         rateLimitAuthMax,
			RateLimitChatMax:         rateLimitChatMax,
			RateLimitBehaviorMax:     rateLimitBehaviorMax,
			RateLimitOnboardMax:      rateLimitOnboardMax,
			RateLimitIdentity:        rateLimitIdentity,
			IdempotencyEnabled:       idempotencyEnabled,
			IdempotencyTTL:           idempotencyTTL,
			StreamMaxConnsPerAccount: streamMaxConnsPerAccount,
			StreamMaxConnsPerSession: streamMaxConnsPerSession,
			StreamAllowQueryAccessToken: streamAllowQueryAccessToken,
			StreamAllowedOrigins:        streamAllowedOrigins,
		}
	}

	return Config{
		HTTPAddr:                 httpAddr,
		DatabaseURL:              databaseURL,
		Postgres:                 postgresCfg,
		PostgresAdminDB:          postgresAdminDB,
		AutoMigrate:              true,
		TickInterval:             tickInterval,
		GameMinutesRate:          gameMinutesRate,
		FrontendDevProxyURL:      frontendDevProxyURL,
		MMOAuthEnabled:           mmoAuthEnabled,
		MMORealmScopingEnabled:   mmoRealmScopingEnabled,
		MMOChatEnabled:           mmoChatEnabled,
		MMOAdminEnabled:          mmoAdminEnabled,
		MMOOTelEnabled:           mmoOTelEnabled,
		OTELServiceName:          otelServiceName,
		OTELEndpoint:             otelEndpoint,
		OTELInsecure:             otelInsecure,
		OTELSampleRatio:          otelSampleRatio,
		MMOJWTIssuer:             mmoJWTIssuer,
		MMOJWTSecret:             mmoJWTSecret,
		MMOAccessTokenTTL:        mmoAccessTokenTTL,
		MMORefreshTokenTTL:       mmoRefreshTokenTTL,
		RateLimitEnabled:         rateLimitEnabled,
		RateLimitWindow:          rateLimitWindow,
		RateLimitAuthMax:         rateLimitAuthMax,
		RateLimitChatMax:         rateLimitChatMax,
		RateLimitBehaviorMax:     rateLimitBehaviorMax,
		RateLimitOnboardMax:      rateLimitOnboardMax,
		RateLimitIdentity:        rateLimitIdentity,
		IdempotencyEnabled:       idempotencyEnabled,
		IdempotencyTTL:           idempotencyTTL,
		StreamMaxConnsPerAccount: streamMaxConnsPerAccount,
		StreamMaxConnsPerSession: streamMaxConnsPerSession,
		StreamAllowQueryAccessToken: streamAllowQueryAccessToken,
		StreamAllowedOrigins:        streamAllowedOrigins,
	}
}

func getCSVOrDefault(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func getEnvOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}

func getDurationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func getFloatOrDefault(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func getBoolOrDefault(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "true" || lower == "1" || lower == "yes" || lower == "on" {
		return true
	}
	if lower == "false" || lower == "0" || lower == "no" || lower == "off" {
		return false
	}

	return fallback
}

func getIntOrDefault(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func getRateLimitIdentityOrDefault(key string, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "ip", "account_or_ip":
		return value
	default:
		return fallback
	}
}

func getOTELSampleRatio(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	if parsed < 0 {
		return 0
	}
	if parsed > 1 {
		return 1
	}

	return parsed
}

func getDefaultOTELSampleRatio(env string) float64 {
	normalized := strings.ToLower(strings.TrimSpace(env))
	if normalized == "production" || normalized == "prod" {
		return 0.1
	}
	return 1.0
}
