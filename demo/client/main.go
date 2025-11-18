// filepath: d:\project\MyFlowHub-Core\demo\client\main.go
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"MyFlowHub-Core/internal/core/header"
)

const (
	subProtoEcho  = 1
	subProtoUpper = 2
)

// 该示例实现一个简单的 TCP 客户端，使用 HeaderTcp 协议：
// 1) 连接到服务端（默认 127.0.0.1:9000，可通过环境变量 DEMO_ADDR 修改）；
// 2) 定期发送带 Header 的消息；
// 3) 接收并解析服务端响应。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	var intervalSec int
	var msgCount int
	flag.IntVar(&intervalSec, "i", 3, "message send interval, seconds")
	flag.IntVar(&msgCount, "n", 5, "number of messages to send (0=infinite)")
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
	defer func() { _ = conn.Close() }()

	// 启用 TCP KeepAlive
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}

	slog.Info("连接成功", "local", conn.LocalAddr(), "remote", conn.RemoteAddr())

	codec := header.HeaderTcpCodec{}

	// 启动接收协程
	go func() {
		for {
			h, payload, err := codec.Decode(conn)
			if err != nil {
				if err == io.EOF {
					slog.Info("服务端关闭连接")
				} else {
					slog.Error("解码失败", "err", err)
				}
				return
			}

			hdr, ok := h.(header.HeaderTcp)
			if !ok {
				slog.Error("header 类型错误")
				continue
			}

			slog.Info("收到响应",
				"major", hdr.Major(),
				"subproto", hdr.SubProto(),
				"msgid", hdr.MsgID,
				"source", fmt.Sprintf("0x%08X", hdr.Source),
				"target", fmt.Sprintf("0x%08X", hdr.Target),
				"payload", string(payload))
		}
	}()

	// 定时发送消息
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	// 生成本地节点 ID（简单示例：使用时间戳低位）
	localID := uint32(time.Now().Unix() & 0xFFFFFF)
	serverID := uint32(0x01020304) // 假设服务端 ID

	sent := 0
	for {
		select {
		case <-ticker.C:
			payload := []byte(fmt.Sprintf("Hello from client, msg #%d", sent))
			hdr := header.HeaderTcp{
				MsgID:      uint32(sent + 1),
				Source:     localID,
				Target:     serverID,
				Timestamp:  uint32(time.Now().Unix()),
				PayloadLen: uint32(len(payload)),
			}
			sub := subProtoEcho
			if sent%2 == 1 {
				sub = subProtoUpper
			}
			hdr.WithMajor(header.MajorMsg).WithSubProto(uint8(sub))

			// 编码并发送
			frame, err := codec.Encode(hdr, payload)
			if err != nil {
				slog.Error("编码失败", "err", err)
				return
			}

			if _, err := conn.Write(frame); err != nil {
				slog.Error("发送失败", "err", err)
				return
			}

			slog.Info("已发送",
				"msgid", hdr.MsgID,
				"subproto", sub,
				"payload", string(payload))

			sent++
			if msgCount > 0 && sent >= msgCount {
				slog.Info("已发送指定数量消息，等待2秒后退出", "count", sent)
				time.Sleep(2 * time.Second)
				return
			}
		}
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
