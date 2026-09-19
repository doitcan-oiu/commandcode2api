package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"commandcode2api/internal/config"
)

// Run listens for requests and owns background maintenance until ctx is canceled.
// The caller closes the proxy's outbound connections with Close after Run returns.
func (p *Proxy) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cfg := p.cfg
	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           p,
		ReadHeaderTimeout: cfg.KeepAliveTimeout + time.Second,
		IdleTimeout:       cfg.KeepAliveTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	p.log("info", "CC Proxy started", M{
		"url":                "http://" + server.Addr,
		"api":                cfg.APIBase,
		"protocolVersion":    config.ProtocolVersion,
		"keepAliveTimeout":   fmt.Sprintf("%dms (反代侧 keepalive_timeout 必须小于它)", cfg.KeepAliveTimeout.Milliseconds()),
		"checkProtocolDrift": cfg.CheckProtocolDrift,
		"maxInflight":        cfg.MaxInflight,
	})
	backgroundDone := make(chan struct{})
	go func() { defer close(backgroundDone); p.background(ctx) }()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if server.Shutdown(shutdownCtx) != nil {
			_ = server.Close()
		}
	}()
	err = server.Serve(listener)
	cancel()
	<-shutdownDone
	<-backgroundDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
