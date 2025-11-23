package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"MyFlowHub-Core/internal/login_server"
)

func main() {
	cfg := login_server.LoadConfigFromEnv()

	nodeID := uint(cfg.NodeID)
	rootNodeID := uint(cfg.RootNodeID)

	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address")
	flag.StringVar(&cfg.DSN, "dsn", cfg.DSN, "Postgres DSN (required)")
	flag.UintVar(&nodeID, "node-id", nodeID, "node id for this login server")
	flag.StringVar(&cfg.ParentAddr, "parent", cfg.ParentAddr, "parent address (root hub)")
	flag.BoolVar(&cfg.ParentEnable, "parent-enable", cfg.ParentEnable, "enable parent link")
	flag.IntVar(&cfg.ParentReconnectSec, "parent-reconnect", cfg.ParentReconnectSec, "parent reconnect seconds")
	flag.StringVar(&cfg.RootToken, "root-token", cfg.RootToken, "root privilege token for registration")
	flag.UintVar(&rootNodeID, "root-node-id", rootNodeID, "root node id target for registration")
	flag.IntVar(&cfg.ProcessChannels, "proc-channels", cfg.ProcessChannels, "process channel count")
	flag.IntVar(&cfg.ProcessWorkers, "proc-workers", cfg.ProcessWorkers, "process workers per channel")
	flag.IntVar(&cfg.ProcessBuffer, "proc-buffer", cfg.ProcessBuffer, "process channel buffer")
	flag.IntVar(&cfg.SendChannels, "send-channels", cfg.SendChannels, "send dispatcher channels")
	flag.IntVar(&cfg.SendWorkers, "send-workers", cfg.SendWorkers, "send dispatcher workers per channel")
	flag.IntVar(&cfg.SendChannelBuffer, "send-channel-buffer", cfg.SendChannelBuffer, "send dispatcher channel buffer")
	flag.IntVar(&cfg.SendConnBuffer, "send-conn-buffer", cfg.SendConnBuffer, "per-connection send buffer")
	flag.Parse()

	cfg.NodeID = uint32(nodeID)
	cfg.RootNodeID = uint32(rootNodeID)
	if cfg.ParentAddr != "" {
		cfg.ParentEnable = true
	}

	log := setupLogger()

	app, err := login_server.NewApp(cfg, log)
	if err != nil {
		log.Error("init login server failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		log.Error("start login server failed", "err", err)
		os.Exit(1)
	}
	log.Info("login server started", "addr", cfg.Addr, "node_id", cfg.NodeID)

	waitSignal()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := app.Stop(stopCtx); err != nil {
		log.Error("stop login server failed", "err", err)
		os.Exit(1)
	}
	log.Info("login server stopped")
}

func setupLogger() *slog.Logger {
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	l := slog.New(handler)
	slog.SetDefault(l)
	return l
}

func waitSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
}
