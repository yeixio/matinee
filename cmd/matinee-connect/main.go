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
	paid := flag.Bool("require-subscription", os.Getenv("CONNECT_REQUIRE_SUBSCRIPTION") == "true", "only serve servers with a paid Matinee Connect subscription; needs CONNECT_STRIPE_KEY and CONNECT_PASS_KEY")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	svc := &connect.Service{ClientIPHeader: *header}
	// Secrets come from the environment only, never flags, which show in
	// the process list.
	stripeKey, passKey := os.Getenv("CONNECT_STRIPE_KEY"), os.Getenv("CONNECT_PASS_KEY")
	if *paid {
		// Fail closed: a service meant to be paid never runs open.
		if stripeKey == "" || len(passKey) < 32 {
			log.Error("a paid service needs CONNECT_STRIPE_KEY and a CONNECT_PASS_KEY of at least 32 characters")
			os.Exit(1)
		}
		svc.Stripe, svc.PassKey = &connect.Stripe{Key: stripeKey}, []byte(passKey)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go svc.Run(ctx)
	srv := &http.Server{Addr: *addr, Handler: svc.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("matinee-connect started", "version", version.Version, "addr", *addr, "subscription", *paid)
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
