// Command matinee-connect runs the rendezvous service that lets the apps
// find a home server whose address changed (internal/connect). It only
// carries addresses, never media.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/connect"
	"github.com/yeixio/matinee/internal/version"
)

func main() {
	addr := flag.String("addr", envOr("CONNECT_ADDR", ":8080"), "listen address")
	header := flag.String("client-ip-header", os.Getenv("CONNECT_CLIENT_IP_HEADER"), "header the proxy in front puts the client's address in, such as X-Real-IP; empty uses the connection's address")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc := &connect.Service{ClientIPHeader: *header}
	go svc.Run(ctx)
	srv := &http.Server{Addr: *addr, Handler: svc.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("matinee-connect started", "version", version.Version, "addr", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("matinee-connect stopped", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
