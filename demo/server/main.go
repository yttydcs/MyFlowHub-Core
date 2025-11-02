package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 该示例实现一个简单的 TCP 长连接服务端，包含：
// 1) 行协议：每条消息以"\n"结尾；
// 2) 心跳：客户端发送 "PING"，服务端回复 "PONG"；
// 3) 超时：为读/写分别设置超时，便于检测断链；
// 4) KeepAlive：启用 TCP KeepAlive，降低被中间设备清理的概率；
// 5) 优雅退出：Ctrl+C 等信号触发关闭监听器与现有连接；
// 6) 地址配置：通过环境变量 DEMO_ADDR（默认 :9000）。
func main() {
	initLoggerFromEnv()

	addr := getenv("DEMO_ADDR", ":9000")
	slog.Info("服务端启动", "listen", addr)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("监听失败", "err", err)
		os.Exit(1)
	}
	// 确保退出前关闭监听器（忽略错误）
	defer func() { _ = ln.Close() }()

	var (
		mu    sync.Mutex                // 保护连接表的互斥锁
		conns = map[net.Conn]struct{}{} // 当前活跃连接集合
		wg    sync.WaitGroup            // 等待所有连接处理协程退出
	)

	// 捕获操作系统信号，用于优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	shutdown := make(chan struct{}) // 关闭通知

	// 收到退出信号后：关闭监听器并主动关闭所有连接
	go func() {
		<-sigCh
		slog.Info("收到退出信号，正在关闭监听器与所有连接")
		close(shutdown)
		_ = ln.Close()
		mu.Lock()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	}()

	// 主循环：接受新的客户端连接
	for {
		conn, err := ln.Accept()
		if err != nil {
			// 如果是我们主动关闭导致的错误，等待所有协程退出后返回
			select {
			case <-shutdown:
				wg.Wait()
				slog.Info("服务端优雅退出完成")
				return
			default:
				// 临时错误重试；其他错误直接退出
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					slog.Warn("Accept 临时错误", "err", ne)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				slog.Error("Accept 失败", "err", err)
				os.Exit(1)
			}
		}

		// 记录连接到集合中
		mu.Lock()
		conns[conn] = struct{}{}
		mu.Unlock()

		// 每个连接启动一个独立的处理协程
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer func() {
				mu.Lock()
				delete(conns, c)
				mu.Unlock()
				_ = c.Close()
			}()

			// 开启 TCP KeepAlive，周期 30 秒
			if tcp, ok := c.(*net.TCPConn); ok {
				_ = tcp.SetKeepAlive(true)
				_ = tcp.SetKeepAlivePeriod(30 * time.Second)
			}

			remote := c.RemoteAddr().String()
			slog.Info("新连接", "remote", remote)

			// 发送欢迎消息
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprintf(c, "WELCOME %s at %s\n", remote, time.Now().Format(time.RFC3339)); err != nil {
				slog.Warn("发送欢迎消息失败", "remote", remote, "err", err)
				return
			}

			reader := bufio.NewReader(c)
			for {
				// 每次读取前设置读超时（60s），用来探测僵尸连接
				if err := c.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
					slog.Warn("设置读超时失败", "remote", remote, "err", err)
					return
				}
				line, err := reader.ReadString('\n') // 行协议：以换行符作为消息边界
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						slog.Warn("读取超时，关闭连接", "remote", remote)
						return
					}
					// 常见断链场景：对端关闭/连接被重置
					if err.Error() == "EOF" || strings.Contains(strings.ToLower(err.Error()), "closed") {
						slog.Info("客户端关闭连接", "remote", remote)
						return
					}
					slog.Error("读取失败", "remote", remote, "err", err)
					return
				}

				msg := strings.TrimSpace(line)
				if msg == "" {
					continue
				}
				slog.Info("收到消息", "remote", remote, "msg", msg)

				// 心跳与回显：PING => PONG，其它内容带时间戳回显
				var reply string
				switch strings.ToUpper(msg) {
				case "PING":
					reply = "PONG"
				default:
					reply = fmt.Sprintf("ECHO[%s]: %s", time.Now().Format(time.RFC3339), msg)
				}

				// 写超时（10s）避免写阻塞
				if err := c.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					slog.Warn("设置写超时失败", "remote", remote, "err", err)
					return
				}
				if _, err := fmt.Fprintf(c, "%s\n", reply); err != nil {
					slog.Error("写入失败", "remote", remote, "err", err)
					return
				}
			}
		}(conn)
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
