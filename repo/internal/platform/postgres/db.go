// Package postgres provides PostgreSQL connection management via pgx.
package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config holds the database connection settings.
type Config struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	MaxConns int32
}

// ConfigFromEnv reads database config from environment variables and
// secret files under SECRETS_DIR.
func ConfigFromEnv() (Config, error) {
	secretsDir := envOrDefault("SECRETS_DIR", "/runtime/secrets")

	password, err := readSecretFile(secretsDir, "db_apppassword.txt")
	if err != nil {
		return Config{}, fmt.Errorf("db config: %w", err)
	}

	return Config{
		Host:     envOrDefault("DB_HOST", "localhost"),
		Port:     envOrDefault("DB_PORT", "5432"),
		Database: envOrDefault("APP_DB", "portal"),
		User:     envOrDefault("APP_USER", "portal_app"),
		Password: strings.TrimSpace(password),
		MaxConns: 20,
	}, nil
}

// Open creates and validates a pgx connection pool.
func Open(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable pool_max_conns=%d",
		cfg.Host, cfg.Port, cfg.Database, cfg.User, cfg.Password, cfg.MaxConns,
	)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Validate connectivity
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func readSecretFile(dir, name string) (string, error) {
	path := dir + "/" + name
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
