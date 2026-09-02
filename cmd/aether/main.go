// Package main is the entry point of the Aether proxy application.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/CelestialLuminary36/Aether/core"
	"github.com/CelestialLuminary36/Aether/inbound/socks"
	"github.com/CelestialLuminary36/Aether/outbound/direct"
)

func main() {
	// Initialize the default structured logger to stdout.
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Create a context that is canceled when an interrupt or termination signal is received.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build the outbound dialer and inbound listener.
	outboundDirect := direct.New()
	socksInbound := socks.New("127.0.0.1:1080")

	slog.Info("Aether Starting...", "version", "1.27.1")

	// Start the SOCKS5 inbound server and handle each accepted connection.
	err := socksInbound.Start(ctx, func(sess *core.Session, inConn net.Conn) {
		defer inConn.Close()

		slog.Debug("Receiving inbound connection", "inbound", socksInbound.Name(), "target", sess.TargetAddrPort)

		// Dial the target address through the direct outbound.
		outConn, err := outboundDirect.DialContext(sess.Context, sess)

		if err != nil {
			slog.Error("Dialing target failed", "target", sess.TargetAddr, "err", err)
			return
		}
		defer outConn.Close()

		// Relay traffic bidirectionally between the inbound and outbound connections.
		core.Relay(inConn, outConn)
	})
	if err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Error("Aether exited unexpectedly", "err", err)
		os.Exit(1)
	}

	slog.Info("Aether listening on 127.0.0.1:1080, press Ctrl+C to exit")
	<-ctx.Done()
	slog.Info("Aether shut down gracefully")
}
