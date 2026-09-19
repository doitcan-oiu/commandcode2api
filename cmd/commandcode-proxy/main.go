package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"commandcode2api/internal/config"
	"commandcode2api/internal/proxy"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to configuration file")
	health := flag.Bool("healthcheck", false, "probe the local /health endpoint and exit")
	flag.Parse()
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *health {
		host := "127.0.0.1"
		if cfg.Host != "" && cfg.Host != "0.0.0.0" && cfg.Host != "::" {
			host = cfg.Host
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.Proxy = nil
		client := &http.Client{Transport: t, Timeout: 2 * time.Second}
		defer client.CloseIdleConnections()
		r, e := client.Get("http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port)) + "/health")
		if e != nil {
			os.Exit(1)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	var output io.Writer = os.Stdout
	if cfg.LogFile != "" {
		file, e := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if e != nil {
			fmt.Fprintln(os.Stderr, "Cannot open log file:", e)
			os.Exit(1)
		}
		defer file.Close()
		output = io.MultiWriter(output, file)
	}
	logger := log.New(output, "", log.Ldate|log.Ltime|log.LUTC)
	p, err := proxy.New(cfg, logger)
	if err != nil {
		logger.Print(err)
		os.Exit(1)
	}
	defer p.Close()
	if cfg.GatewayEnabled {
		if err = p.EnableGateway(); err != nil {
			logger.Print(err)
			os.Exit(1)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = p.Run(ctx); err != nil {
		logger.Print(err)
		os.Exit(1)
	}
}
