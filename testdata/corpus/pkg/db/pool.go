package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PoolConfig specifies parameters for database connection pool management.
type PoolConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	MaxRetries      int
	BaseBackoff     time.Duration
}

// ConnectionPool manages active SQL database handles with health monitoring and retry.
type ConnectionPool struct {
	DB     *sql.DB
	Config PoolConfig
}

// NewConnectionPool initializes the database pool with configured limits and backoff retry.
func NewConnectionPool(ctx context.Context, cfg PoolConfig) (*ConnectionPool, error) {
	var (
		db  *sql.DB
		err error
	)

	backoff := cfg.BaseBackoff
	if backoff == 0 {
		backoff = 100 * time.Millisecond
	}

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		db, err = sql.Open("postgres", cfg.DSN)
		if err == nil {
			db.SetMaxOpenConns(cfg.MaxOpenConns)
			db.SetMaxIdleConns(cfg.MaxIdleConns)
			db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
			db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = db.PingContext(pingCtx)
			cancel()
			if err == nil {
				return &ConnectionPool{DB: db, Config: cfg}, nil
			}
			_ = db.Close()
		}

		time.Sleep(backoff)
		backoff *= 2
	}

	return nil, fmt.Errorf("exhausted connection retries: %w", err)
}

// HealthCheck performs a ping to verify database responsiveness.
func (p *ConnectionPool) HealthCheck(ctx context.Context) error {
	if p.DB == nil {
		return fmt.Errorf("connection pool is uninitialized")
	}
	return p.DB.PingContext(ctx)
}

// Close gracefully terminates all connections in the pool.
func (p *ConnectionPool) Close() error {
	if p.DB != nil {
		return p.DB.Close()
	}
	return nil
}
