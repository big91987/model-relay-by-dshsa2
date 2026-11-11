// Package config loads runtime configuration from the environment.
package config

import "os"

// Config holds the gateway runtime configuration.
type Config struct {
	Port          string
	DatabasePath  string
	AdminUsername string
	AdminPassword string
	JWTSecret     string
	WebDist       string
}

// Load reads configuration from environment variables, applying local defaults.
func Load() Config {
	return Config{
		Port:          getenv("PORT", "8080"),
		DatabasePath:  getenv("DATABASE_PATH", "./data/gateway.db"),
		AdminUsername: getenv("ADMIN_USERNAME", "admin"),
		AdminPassword: getenv("ADMIN_PASSWORD", "admin123"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		WebDist:       getenv("WEB_DIST", "web/dist"),
	}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
