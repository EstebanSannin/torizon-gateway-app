// Command gateway-manager is the Torizon Gateway on-device management app.
// It serves an embedded web UI over HTTPS and (progressively) manages the
// board's system info, network, containers, and offline updates.
//
// Phase-0 scaffold: HTTPS server + dashboard reading the HAL. See docs/ARCHITECTURE.md.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/auth"
	"github.com/toradex/torizon-gateway-app/internal/config"
	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/hal"
	"github.com/toradex/torizon-gateway-app/internal/httpserver"
	"github.com/toradex/torizon-gateway-app/internal/network"
	"github.com/toradex/torizon-gateway-app/internal/store"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit 0 (healthy) or 1")
	nmWriteTest := flag.Bool("nmwrite-selftest", false, "test whether NetworkManager writes are permitted (polkit), then exit")
	flag.Parse()

	cfg := config.Load()

	// Container HEALTHCHECK entrypoint: hit our own /healthz and exit.
	if *healthcheck {
		os.Exit(runHealthcheck(cfg.ListenAddr))
	}

	// One-off diagnostic: can we write to NetworkManager from here (polkit)?
	if *nmWriteTest {
		if err := network.New(cfg.DBusSocket).WriteProbe(); err != nil {
			log.Printf("NM write NOT permitted: %v", err)
			os.Exit(1)
		}
		log.Println("NM write permitted (checkpoint create/destroy succeeded)")
		os.Exit(0)
	}
	hal.SetHostRoot(cfg.HostRoot)
	board := hal.Detect()
	log.Printf("torizon-gateway starting: board=%s model=%q os=%q",
		board.Kind(), board.Model(), board.OSVersion())

	// First-boot: generate a self-signed cert if none exists.
	if err := httpserver.EnsureSelfSignedCert(cfg.TLSCertFile, cfg.TLSKeyFile, cfg.Hostname, cfg.TLSSANs); err != nil {
		log.Fatalf("tls setup: %v", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	authSvc := auth.New(st, cfg.SessionTTL)
	cnt := containers.New(cfg.DockerSocket)
	net := network.New(cfg.DBusSocket)
	srv := httpserver.New(cfg, board, cnt, net, authSvc, st)
	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on https://%s (hostname: %s.local)", cfg.ListenAddr, cfg.Hostname)
		err := httpSrv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
		os.Exit(1)
	}
}

// runHealthcheck performs a local HTTPS GET to /healthz. Returns 0 if the server
// answers 200, else 1. Skips cert verification (self-signed, loopback only).
func runHealthcheck(listenAddr string) int {
	// listenAddr is like ":8443" — probe it on loopback.
	host := "127.0.0.1" + listenAddr
	if !strings.HasPrefix(listenAddr, ":") {
		host = listenAddr
	}
	client := &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Get("https://" + host + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return 0
	}
	fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
	return 1
}
