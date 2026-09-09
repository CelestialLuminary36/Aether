// Package main is the Aether proxy entry point.
//
// Plan 1 version: wires SOCKS5 in + direct out through the
// StaticDispatcher. No sniffing, routing, DNS, or config file yet.
// Those arrive in later plans.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/CelestialLuminary36/Aether/dispatcher"
	"github.com/CelestialLuminary36/Aether/inbound/socks"
	"github.com/CelestialLuminary36/Aether/outbound/block"
	"github.com/CelestialLuminary36/Aether/outbound/direct"
)

const (
	listenAddr = "127.0.0.1:1080"
	adminUser  = "admin"
	adminPass  = "123456"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Outbounds.
	out := direct.New("out-direct")
	blk := block.New("out-block")

	// Dispatcher: Plan 1 always uses the primary outbound.
	// TODO(user): in Plan 2 replace StaticDispatcher with the real
	// pipeline dispatcher that uses route.Router + dns.Resolver.
	disp := dispatcher.NewStaticDispatcher(out, blk)

	// Inbound: SOCKS5 with password auth.
	in := socks.New(
		"in-socks",
		listenAddr,
		socks.AuthPassword,
		map[string]string{adminUser: adminPass},
		disp,
	)

	slog.Info("Aether starting", "version", "1.27.1")

	if err := in.Start(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Error("Aether exited unexpectedly", "err", err)
		os.Exit(1)
	}

	slog.Info("Aether listening", "addr", listenAddr, "auth", "password")

	<-ctx.Done()
	slog.Info("Aether shutting down")
	_ = in.Close()
	slog.Info("Aether shut down gracefully")
}
