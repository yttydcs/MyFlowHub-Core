// filepath: d:\project\MyFlowHub-Core\demo\server\main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/core/listener/tcp_listener"
	"MyFlowHub-Core/internal/core/server"
)

// 该示例使用 MyFlowHub-Core 框架实现一个 TCP 服务端：
// 1) 使用 HeaderTcp 协议进行消息帧编解码；
// 2) 支持消息回显：收到什么返回什么，payload 前缀 "ECHO: "；
// 3) 优雅退出：Ctrl+C 等信号触发关闭；
// 4) 地址配置：通过环境变量 DEMO_ADDR（默认 :9000）。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	slog.Info("服务端启动", "listen", addr)

	// 创建配置
	cfg := config.NewMap(map[string]string{
		"addr": addr,
	})

	// 创建连接管理器
	cm := connmgr.New()

	// 创建自定义 Process：收到消息后回显
	proc := &EchoProcess{logger: slog.Default()}

	// 创建 TCP 监听器
	listener := tcp_listener.New(addr, tcp_listener.Options{
		KeepAlive:       true,
		KeepAlivePeriod: 30 * time.Second,
		Logger:          slog.Default(),
	})

	// 创建 HeaderTcp 编解码器
	codec := header.HeaderTcpCodec{}

	// 创建 Server
	srv, err := server.New(server.Options{
		Name:     "EchoServer",
		Logger:   slog.Default(),
		Process:  proc,
		Codec:    codec,
		Listener: listener,
		Config:   cfg,
		Manager:  cm,
	})
	if err != nil {
		slog.Error("创建服务失败", "err", err)
		os.Exit(1)
	}

	// 启动服务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := srv.Start(ctx); err != nil {
		slog.Error("启动服务失败", "err", err)
		os.Exit(1)
	}

	slog.Info("服务器已启动", "addr", listener.Addr())

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	slog.Info("收到退出信号，正在关闭服务器")

	// 优雅停止
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	if err := srv.Stop(stopCtx); err != nil {
		slog.Error("停止服务失败", "err", err)
	}
	slog.Info("服务器已停止")
}

// EchoProcess 实现回显功能的处理器
type EchoProcess struct {
	logger *slog.Logger
}

func (p *EchoProcess) OnListen(conn core.IConnection) {
	p.logger.Info("新连接建立", "id", conn.ID(), "remote", conn.RemoteAddr())
}

func (p *EchoProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) {
	p.logger.Info("收到消息",
		"conn", conn.ID(),
		"payload_len", len(payload),
		"payload", string(payload))

	// 解析 header
	h, ok := hdr.(header.HeaderTcp)
	if !ok {
		p.logger.Error("header 类型错误")
		return
	}

	// 构造回显响应
	respPayload := []byte(fmt.Sprintf("ECHO: %s", string(payload)))
	respHeader := header.HeaderTcp{
		MsgID:      h.MsgID, // 使用相同的 MsgID
		Source:     h.Target,
		Target:     h.Source,
		Timestamp:  uint32(time.Now().Unix()),
		PayloadLen: uint32(len(respPayload)),
	}
	respHeader.WithMajor(header.MajorOKResp).WithSubProto(h.SubProto())

	// 获取 server 并发送响应
	if srv, ok := ctx.Value("server").(core.IServer); ok && srv != nil {
		if err := srv.Send(ctx, conn.ID(), respHeader, respPayload); err != nil {
			p.logger.Error("发送响应失败", "err", err)
		}
	} else {
		// 直接通过连接发送
		codec := header.HeaderTcpCodec{}
		if err := conn.SendWithHeader(respHeader, respPayload, codec); err != nil {
			p.logger.Error("发送响应失败", "err", err)
		}
	}
}

func (p *EchoProcess) OnSend(_ context.Context, conn core.IConnection, _ header.IHeader, payload []byte) error {
	p.logger.Debug("发送消息", "conn", conn.ID(), "bytes", len(payload))
	return nil
}

func (p *EchoProcess) OnClose(conn core.IConnection) {
	p.logger.Info("连接关闭", "id", conn.ID())
}

// getenv 读取环境变量，不存在则返回默认值
func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// initLoggerFromEnv 基于环境变量初始化 slog 的默认 Logger。
// 支持：LOG_LEVEL=DEBUG|INFO|WARN|ERROR（默认 INFO）
//
//	LOG_JSON=true|false（默认 false）
//	LOG_CALLER=true|false（默认 false）
func initLoggerFromEnv() {
	lv := strings.TrimSpace(strings.ToUpper(getenv("LOG_LEVEL", "INFO")))
	jsonOut := parseBool(getenv("LOG_JSON", "false"), false)
	addSource := parseBool(getenv("LOG_CALLER", "false"), false)

	level := new(slog.LevelVar)
	switch lv {
	case "DEBUG":
		level.Set(slog.LevelDebug)
	case "WARN", "WARNING":
		level.Set(slog.LevelWarn)
	case "ERROR":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}

	h := slog.HandlerOptions{Level: level, AddSource: addSource}
	var handler slog.Handler
	if jsonOut {
		handler = slog.NewJSONHandler(os.Stdout, &h)
	} else {
		handler = slog.NewTextHandler(os.Stdout, &h)
	}
	slog.SetDefault(slog.New(handler))
}

func parseBool(s string, def bool) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "true" || s == "1" || s == "yes" || s == "y" {
		return true
	}
	if s == "false" || s == "0" || s == "no" || s == "n" {
		return false
	}
	return def
}
