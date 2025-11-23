package login_server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/process"
	"MyFlowHub-Core/internal/core/server"
	"MyFlowHub-Core/internal/handler"
)

type App struct {
	cfg       Config
	log       *slog.Logger
	store     Store
	registrar *Registrar
	srv       *server.Server
}

func NewApp(cfg Config, log *slog.Logger) (*App, error) {
	if cfg.DSN == "" {
		return nil, errors.New("dsn required")
	}
	if cfg.Addr == "" {
		cfg.Addr = ":9100"
	}
	if cfg.RootNodeID == 0 {
		cfg.RootNodeID = 1
	}
	if log == nil {
		log = slog.Default()
	}
	store, err := NewPostgresStore(cfg.DSN)
	if err != nil {
		return nil, err
	}
	reg := NewRegistrar(cfg.RootToken, cfg.RootNodeID, log)
	app := &App{
		cfg:       cfg,
		log:       log,
		store:     store,
		registrar: reg,
	}
	if err := app.initServer(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return app, nil
}

func (a *App) initServer() error {
	cfgMap := a.cfg.toMapConfig()
	cm := connmgr.New()
	base := process.NewPreRoutingProcess(a.log).WithConfig(cfgMap)
	dispatcher, err := process.NewDispatcherFromConfig(cfgMap, base, a.log)
	if err != nil {
		return err
	}
	if err := dispatcher.RegisterHandler(NewAuthorityHandler(a.store, a.log)); err != nil {
		return err
	}
	dispatcher.RegisterDefaultHandler(handler.NewDefaultForwardHandler(cfgMap, a.log))

	proc := NewProcessWrapper(dispatcher, a.registrar)
	lst := tcp_listener.New(a.cfg.Addr, tcp_listener.Options{
		KeepAlive:       true,
		KeepAlivePeriod: 30 * time.Second,
		Logger:          a.log,
	})
	codec := header.HeaderTcpCodec{}
	srv, err := server.New(server.Options{
		Name:     "LoginServer",
		Logger:   a.log,
		Process:  proc,
		Codec:    codec,
		Listener: lst,
		Config:   cfgMap,
		Manager:  cm,
		NodeID:   a.cfg.NodeID,
	})
	if err != nil {
		return err
	}
	proc.SetServerProvider(func() core.IServer { return srv })
	a.srv = srv
	return nil
}

func (a *App) Start(ctx context.Context) error {
	if a.srv == nil {
		return errors.New("server not initialized")
	}
	return a.srv.Start(ctx)
}

func (a *App) Stop(ctx context.Context) error {
	var first error
	if a.srv != nil {
		if err := a.srv.Stop(ctx); err != nil && first == nil {
			first = err
		}
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func LoadConfigFromEnv() Config {
	return Config{
		Addr:               getenv("LOGIN_ADDR", ":9100"),
		DSN:                getenv("LOGIN_PG_DSN", ""),
		NodeID:             getenvUint32("LOGIN_NODE_ID", 2),
		ParentAddr:         getenv("LOGIN_PARENT_ADDR", ""),
		ParentEnable:       parseBool(getenv("LOGIN_PARENT_ENABLE", ""), false),
		ParentReconnectSec: int(getenvInt("LOGIN_PARENT_RECONNECT", 3)),
		RootToken:          getenv("LOGIN_ROOT_TOKEN", ""),
		RootNodeID:         getenvUint32("LOGIN_ROOT_NODE_ID", 1),
		ProcessChannels:    int(getenvInt("LOGIN_PROC_CHANNELS", 2)),
		ProcessWorkers:     int(getenvInt("LOGIN_PROC_WORKERS", 2)),
		ProcessBuffer:      int(getenvInt("LOGIN_PROC_BUFFER", 128)),
		SendChannels:       int(getenvInt("LOGIN_SEND_CHANNELS", 1)),
		SendWorkers:        int(getenvInt("LOGIN_SEND_WORKERS", 1)),
		SendChannelBuffer:  int(getenvInt("LOGIN_SEND_CHANNEL_BUFFER", 64)),
		SendConnBuffer:     int(getenvInt("LOGIN_SEND_CONN_BUFFER", 64)),
	}
}

func parseBool(v string, def bool) bool {
	if v == "" {
		return def
	}
	return core.ParseBool(v, def)
}
