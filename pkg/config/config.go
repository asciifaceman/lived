package config

import (
	"fmt"
	"os"
	"strconv"
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
	HTTPAddr            string
	DatabaseURL         string
	Postgres            PostgresConfig
	PostgresAdminDB     string
	AutoMigrate         bool
	TickInterval        time.Duration
	GameMinutesRate     float64
	FrontendDevProxyURL string
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

	databaseURL := os.Getenv("LIVED_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = postgresCfg.DSN()
	}

	autoMigrate := os.Getenv("LIVED_AUTO_MIGRATE")
	if autoMigrate == "false" || autoMigrate == "0" {
		return Config{
			HTTPAddr:            httpAddr,
			DatabaseURL:         databaseURL,
			Postgres:            postgresCfg,
			PostgresAdminDB:     postgresAdminDB,
			AutoMigrate:         false,
			TickInterval:        tickInterval,
			GameMinutesRate:     gameMinutesRate,
			FrontendDevProxyURL: frontendDevProxyURL,
		}
	}

	return Config{
		HTTPAddr:            httpAddr,
		DatabaseURL:         databaseURL,
		Postgres:            postgresCfg,
		PostgresAdminDB:     postgresAdminDB,
		AutoMigrate:         true,
		TickInterval:        tickInterval,
		GameMinutesRate:     gameMinutesRate,
		FrontendDevProxyURL: frontendDevProxyURL,
	}
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
