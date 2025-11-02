# TCP Long Connection Demo

该示例展示了使用标准库 slog 的 TCP 服务端与客户端，包含长连接、心跳(PING/PONG)、超时与优雅退出。

## 运行

在 Windows cmd 下：

```cmd
set LOG_LEVEL=INFO
set LOG_JSON=false
set LOG_CALLER=true

set DEMO_ADDR=:9000
cd demo\server
go run .
```

新开终端：

```cmd
set LOG_LEVEL=DEBUG
set LOG_JSON=false
set LOG_CALLER=true

set DEMO_ADDR=:9000
cd demo\client
go run . -i 5
```

- LOG_LEVEL: DEBUG | INFO | WARN | ERROR（默认 INFO）
- LOG_JSON: true 切换为 JSON 输出；false 为文本（默认 false）
- LOG_CALLER: 是否输出调用方（默认 true）

## 说明

- 行协议：以换行结尾；客户端周期发送 PING 和文本消息，服务端回复 PONG 或 ECHO。
- 读写超时：服务端读 60s/写 10s；客户端读 65s/写 5s。
- KeepAlive：双端开启，周期 30s。
- 优雅退出：Ctrl+C 关闭监听器与连接。
