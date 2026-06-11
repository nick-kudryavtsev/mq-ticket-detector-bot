// Package repository — единственная точка доступа к PostgreSQL.
// Все запросы идут через пул соединений pgxpool; ни один другой
// пакет не знает о SQL и драйвере.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// db — общий интерфейс для *pgxpool.Pool и pgx.Tx: репозитории не знают,
// выполняются они на пуле или внутри транзакции.
type db interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewPool создаёт пул соединений и проверяет доступность БД одним ping.
// Размер пула pgx по умолчанию — max(4, GOMAXPROCS), для сервера
// на 3 vCPU этого достаточно.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
