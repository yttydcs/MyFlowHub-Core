package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// 该示例实现一个简单的 TCP 长连接客户端，包含：
// 1) 连接到服务端（默认 127.0.0.1:9000，可通过环境变量 DEMO_ADDR 修改）；
// 2) 开启 TCP KeepAlive；
// 3) 启动读循环，接收服务端欢迎消息与后续响应；
// 4) 定时发送心跳 PING 与示例业务消息；
// 5) 设置读写超时，避免读写永久阻塞。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	var intervalSec int
	flag.IntVar(&intervalSec, "i", 10, "heartbeat and message interval, seconds")
	flag.Parse()

	// 当地址仅提供端口时，默认连到本机
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	slog.Info("开始连接", "addr", addr)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		slog.Error("连接失败", "err", err)
		os.Exit(1)
	}
	// 退出时关闭连接（忽略错误）
	defer func() { _ = conn.Close() }()

	// 启用 TCP KeepAlive，周期 30 秒
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}

	reader := bufio.NewReader(conn)
	// 首次读取：服务端欢迎消息
	if line, err := reader.ReadString('\n'); err == nil {
		slog.Info("收到欢迎消息", "greeting", strings.TrimSpace(line))
	} else {
		slog.Warn("读取欢迎消息失败", "err", err)
	}

	// 读协程：持续读取服务端响应
	go func() {
		for {
			// 每次读取前设置读超时（65s），略长于服务端设置
			if err := conn.SetReadDeadline(time.Now().Add(65 * time.Second)); err != nil {
				slog.Warn("设置读超时失败", "err", err)
				return
			}
			line, err := reader.ReadString('\n') // 行协议：以换行符作为消息边界
			if err != nil {
				slog.Error("读取失败", "err", err)
				return
			}
			slog.Info("收到响应", "data", strings.TrimSpace(line))
		}
	}()

	// 定时发送：先发送心跳 PING，再发送示例业务消息
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()
	count := 0
	for range ticker.C {
		msgs := []string{"PING", fmt.Sprintf("hello #%d", count)}
		for _, m := range msgs {
			// 每次写入前设置写超时（5s），避免写阻塞
			if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				slog.Warn("设置写超时失败", "err", err)
				return
			}
			if _, err := fmt.Fprintf(conn, "%s\n", m); err != nil {
				slog.Error("写入失败", "err", err)
				return
			}
			slog.Debug("已发送", "msg", m)
		}
		count++
	}
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
//	LOG_CALLER=true|false（默认 true）
func initLoggerFromEnv() {
	lv := strings.TrimSpace(strings.ToUpper(getenv("LOG_LEVEL", "INFO")))
	jsonOut := parseBool(getenv("LOG_JSON", "false"), false)
	addSource := parseBool(getenv("LOG_CALLER", "true"), true)

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
