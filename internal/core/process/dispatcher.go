package process

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"sync"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/header"
)

// DispatchOptions 定义 DispatcherProcess 的运行参数。
type DispatchOptions struct {
	Logger         *slog.Logger
	ChannelCount   int
	WorkersPerChan int
	ChannelBuffer  int
	Base           core.IProcess
}

type dispatchEvent struct {
	ctx     context.Context
	conn    core.IConnection
	hdr     header.IHeader
	payload []byte
}

// DispatcherProcess 提供基于子协议路由的处理管线，支持多通道+多 worker 并发。
type DispatcherProcess struct {
	log      *slog.Logger
	base     core.IProcess
	handlers map[uint8]core.ISubProcess

	queues         []chan dispatchEvent
	chanCount      int
	workersPerChan int

	startOnce  sync.Once
	runtimeCtx context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
}

// NewDispatcher 构建 DispatcherProcess。
func NewDispatcher(opts DispatchOptions) (*DispatcherProcess, error) {
	if opts.ChannelCount <= 0 {
		opts.ChannelCount = 1
	}
	if opts.WorkersPerChan <= 0 {
		opts.WorkersPerChan = 1
	}
	if opts.ChannelBuffer < 0 {
		opts.ChannelBuffer = 0
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	queues := make([]chan dispatchEvent, opts.ChannelCount)
	for i := range queues {
		queues[i] = make(chan dispatchEvent, opts.ChannelBuffer)
	}
	return &DispatcherProcess{
		log:            log,
		base:           opts.Base,
		handlers:       make(map[uint8]core.ISubProcess),
		queues:         queues,
		chanCount:      opts.ChannelCount,
		workersPerChan: opts.WorkersPerChan,
	}, nil
}

// NewDispatcherFromConfig 根据配置创建 DispatcherProcess。
func NewDispatcherFromConfig(cfg core.IConfig, base core.IProcess, logger *slog.Logger) (*DispatcherProcess, error) {
	opts := DispatchOptions{
		Logger:         logger,
		Base:           base,
		ChannelCount:   readPositiveInt(cfg, coreconfig.KeyProcChannelCount, 1),
		WorkersPerChan: readPositiveInt(cfg, coreconfig.KeyProcWorkersPerChan, 1),
		ChannelBuffer:  readPositiveInt(cfg, coreconfig.KeyProcChannelBuffer, 64),
	}
	return NewDispatcher(opts)
}

// RegisterHandler 注册子协议处理器。
func (p *DispatcherProcess) RegisterHandler(h core.ISubProcess) error {
	if h == nil {
		return errors.New("sub process nil")
	}
	sub := h.SubProto()
	if sub > 63 {
		return fmt.Errorf("sub proto %d out of range", sub)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.handlers[sub]; exists {
		return fmt.Errorf("sub proto %d already registered", sub)
	}
	p.handlers[sub] = h
	return nil
}

// ensureRuntime 启动 worker 池。
func (p *DispatcherProcess) ensureRuntime(ctx context.Context) {
	p.startOnce.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		runtimeCtx, cancel := context.WithCancel(ctx)
		p.runtimeCtx = runtimeCtx
		p.cancel = cancel
		for i := range p.queues {
			queue := p.queues[i]
			p.wg.Add(1)
			go func(q chan dispatchEvent) {
				defer p.wg.Done()
				workers := p.workersPerChan
				var wg sync.WaitGroup
				wg.Add(workers)
				for k := 0; k < workers; k++ {
					go func() {
						defer wg.Done()
						for evt := range q {
							p.route(evt)
						}
					}()
				}
				<-runtimeCtx.Done()
				close(q)
				wg.Wait()
			}(queue)
		}
	})
}

func (p *DispatcherProcess) route(evt dispatchEvent) {
	if p.base != nil {
		p.base.OnReceive(evt.ctx, evt.conn, evt.hdr, evt.payload)
	}
	sub, ok := extractSubProto(evt.hdr)
	if !ok {
		if p.base == nil {
			p.log.Warn("header lacks SubProto", "conn", evt.conn.ID())
		}
		return
	}
	handler := p.getHandler(sub)
	if handler == nil {
		if p.base == nil {
			p.log.Warn("no handler for sub proto", "subproto", sub, "conn", evt.conn.ID())
		}
		return
	}
	handler.OnReceive(evt.ctx, evt.conn, evt.hdr, evt.payload)
}

func extractSubProto(h header.IHeader) (uint8, bool) {
	if h == nil {
		return 0, false
	}
	return h.SubProto(), true
}

func (p *DispatcherProcess) getHandler(sub uint8) core.ISubProcess {
	p.mu.RLock()
	h := p.handlers[sub]
	p.mu.RUnlock()
	return h
}

// Shutdown 关闭 worker 池。
func (p *DispatcherProcess) Shutdown() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

// ConfigSnapshot 返回当前通道/worker 配置，便于观测与测试。
func (p *DispatcherProcess) ConfigSnapshot() (channels, workers, buffer int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	channels = len(p.queues)
	workers = p.workersPerChan
	if channels > 0 {
		buffer = cap(p.queues[0])
	}
	return
}

func (p *DispatcherProcess) selectQueue(conn core.IConnection, hdr header.IHeader) int {
	if p.chanCount == 1 {
		return 0
	}
	if conn != nil {
		h := fnv.New32a()
		_, _ = h.Write([]byte(conn.ID()))
		return int(h.Sum32() % uint32(p.chanCount))
	}
	if sub, ok := extractSubProto(hdr); ok {
		return int(sub) % p.chanCount
	}
	return 0
}

// OnListen 实现 core.IProcess。
func (p *DispatcherProcess) OnListen(conn core.IConnection) {
	if p.base != nil {
		p.base.OnListen(conn)
	}
}

// OnReceive 将事件写入通道，供 worker 并发处理。
func (p *DispatcherProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) {
	if ctx == nil {
		ctx = context.Background()
	}
	p.ensureRuntime(ctx)
	idx := p.selectQueue(conn, hdr)
	evt := dispatchEvent{ctx: ctx, conn: conn, hdr: hdr, payload: payload}
	select {
	case p.queues[idx] <- evt:
	case <-ctx.Done():
	case <-p.runtimeCtx.Done():
	}
}

func (p *DispatcherProcess) OnSend(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) error {
	if p.base != nil {
		return p.base.OnSend(ctx, conn, hdr, payload)
	}
	return nil
}

func (p *DispatcherProcess) OnClose(conn core.IConnection) {
	if p.base != nil {
		p.base.OnClose(conn)
	}
}

func readPositiveInt(cfg core.IConfig, key string, def int) int {
	if cfg == nil {
		return def
	}
	if raw, ok := cfg.Get(key); ok {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return def
}
