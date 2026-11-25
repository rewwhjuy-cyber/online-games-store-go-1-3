package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
func Migrate(ctx context.Context, p *pgxpool.Pool, dir string) error {
	files, e := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if e != nil {
		return e
	}
	sort.Strings(files)
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			return e
		}
		name := filepath.Base(f)
		tag, e := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
		_ = tag
		if e != nil {
			return e
		}
		var exists bool
		e = p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&exists)
		if e != nil {
			return e
		}
		if exists {
			continue
		}
		tx, e := p.Begin(ctx)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, string(b)); e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name)
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, e)
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
func SafeSort(s string) string {
	switch strings.ToLower(s) {
	case "price_asc":
		return "price_cents ASC"
	case "price_desc":
		return "price_cents DESC"
	case "title":
		return "title ASC"
	case "newest":
		return "created_at DESC"
	default:
		return "created_at DESC"
	}
}
