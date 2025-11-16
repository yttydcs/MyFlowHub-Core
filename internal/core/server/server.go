package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/reader"
)

// ReaderFactory 创建 IReader。
type ReaderFactory func(conn core.IConnection) core.IReader

// Options 配置 Server。
type Options struct {
	Name          string
	Logger        *slog.Logger
	Process       core.IProcess
	Codec         core.IHeaderCodec
	Listener      core.IListener
	Config        core.IConfig
	Manager       core.IConnectionManager
	ReaderFactory ReaderFactory
}

// Server 是 IServer 的具体实现，负责协调 listener/manager/process。
type Server struct {
	opts  Options
	log   *slog.Logger
	cm    core.IConnectionManager
	proc  core.IProcess
	codec core.IHeaderCodec
	cfg   core.IConfig
	lst   core.IListener
	rFac  ReaderFactory

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
	start  bool
}

// New 构建 Server。
func New(opts Options) (*Server, error) {
	if opts.Listener == nil {
		return nil, errors.New("listener required")
	}
	if opts.Manager == nil {
		return nil, errors.New("manager required")
	}
	if opts.Codec == nil {
		return nil, errors.New("codec required")
	}
	if opts.Process == nil {
		return nil, errors.New("process required")
	}
	if opts.Config == nil {
		return nil, errors.New("config required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ReaderFactory == nil {
		opts.ReaderFactory = func(core.IConnection) core.IReader {
			return reader.NewTCP(opts.Logger)
		}
	}
	return &Server{
		opts:  opts,
		log:   opts.Logger,
		cm:    opts.Manager,
		proc:  opts.Process,
		codec: opts.Codec,
		cfg:   opts.Config,
		lst:   opts.Listener,
		rFac:  opts.ReaderFactory,
	}, nil
}

// Start 启动监听与连接循环。
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.start {
		return errors.New("server already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.cm.SetHooks(core.ConnectionHooks{
		OnAdd: func(conn core.IConnection) {
			s.proc.OnListen(conn)
			s.wg.Add(1)
			go s.serveConn(conn)
		},
		OnRemove: func(conn core.IConnection) {
			s.proc.OnClose(conn)
		},
	})
	s.start = true
	go func() {
		if err := s.lst.Listen(s.ctx, s.cm); err != nil {
			s.log.Error("listener exited", "err", err)
			s.Stop(context.Background())
		}
	}()
	return nil
}

func (s *Server) serveConn(conn core.IConnection) {
	defer s.wg.Done()
	conn.OnReceive(func(c core.IConnection, hdr header.IHeader, payload []byte) {
		s.proc.OnReceive(s.ctx, c, hdr, payload)
	})
	reader := conn.Reader()
	if reader == nil {
		reader = s.rFac(conn)
		conn.SetReader(reader)
	}
	if reader == nil {
		s.log.Error("no reader available", "conn", conn.ID())
		return
	}
	if err := reader.ReadLoop(s.ctx, conn, s.codec); err != nil {
		s.log.Warn("read loop exit", "conn", conn.ID(), "err", err)
	}
	if err := s.cm.Remove(conn.ID()); err != nil {
		s.log.Debug("remove conn", "conn", conn.ID(), "err", err)
	}
}

// Stop 停止服务并释放资源。
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.start {
		s.mu.Unlock()
		return nil
	}
	s.start = false
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	_ = s.lst.Close()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.cm.CloseAll()
}

func (s *Server) Config() core.IConfig                 { return s.cfg }
func (s *Server) ConnManager() core.IConnectionManager { return s.cm }
func (s *Server) Process() core.IProcess               { return s.proc }
func (s *Server) HeaderCodec() core.IHeaderCodec       { return s.codec }

func (s *Server) Send(ctx context.Context, connID string, hdr header.IHeader, payload []byte) error {
	conn, ok := s.cm.Get(connID)
	if !ok {
		return errors.New("conn not found")
	}
	if err := s.proc.OnSend(ctx, conn, hdr, payload); err != nil {
		return err
	}
	return conn.SendWithHeader(hdr, payload, s.codec)
}
