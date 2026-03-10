//go:build ignore

// Package config provides configuration management for the Acme Widget Service.
//
// Configuration is loaded from environment variables with sensible defaults.
// The service requires a PostgreSQL database and optionally uses Redis for caching.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all service configuration.
type Config struct {
	// Database settings
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Redis settings (optional, disables caching if empty)
	RedisURL string

	// Server settings
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Rate limiting
	RateLimit    int           // requests per window
	RateWindow   time.Duration // sliding window duration

	// Authentication
	APIKeyHeader string

	// Logging
	LogLevel string
}

// DefaultConfig returns configuration with defaults, overridden by env vars.
func DefaultConfig() *Config {
	return &Config{
		DBHost:       envOr("WIDGET_DB_HOST", "localhost"),
		DBPort:       envOrInt("WIDGET_DB_PORT", 5432),
		DBUser:       envOr("WIDGET_DB_USER", "widget"),
		DBPassword:   os.Getenv("WIDGET_DB_PASSWORD"),
		DBName:       envOr("WIDGET_DB_NAME", "widgets"),
		DBSSLMode:    envOr("WIDGET_DB_SSLMODE", "disable"),
		RedisURL:     os.Getenv("WIDGET_REDIS_URL"),
		Port:         envOrInt("WIDGET_PORT", 8080),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		RateLimit:    envOrInt("WIDGET_RATE_LIMIT", 100),
		RateWindow:   1 * time.Minute,
		APIKeyHeader: "X-API-Key",
		LogLevel:     envOr("WIDGET_LOG_LEVEL", "info"),
	}
}

// DSN returns the PostgreSQL connection string.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

// Validate checks required configuration fields.
func (c *Config) Validate() error {
	if c.DBPassword == "" {
		return fmt.Errorf("WIDGET_DB_PASSWORD is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid port: %d", c.Port)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
