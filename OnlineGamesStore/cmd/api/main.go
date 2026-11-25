package main

import (
	"context"
	"github.com/vasilenko/online-games-store/internal/config"
	"github.com/vasilenko/online-games-store/internal/db"
	"github.com/vasilenko/online-games-store/internal/httpapi"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, e := db.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		log.Fatalf("database connection: %v", e)
	}
	defer pool.Close()
	if e = db.Migrate(ctx, pool, "migrations"); e != nil {
		log.Fatalf("migrations: %v", e)
	}
	srv := &http.Server{Addr: cfg.Addr, Handler: httpapi.New(pool, cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Printf("Online Games Store API listening on %s", cfg.Addr)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatalf("http server: %v", e)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := srv.Shutdown(shutdown); e != nil {
		log.Printf("shutdown: %v", e)
	}
}
