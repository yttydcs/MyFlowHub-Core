# MyFlowHub-Core

一个基于 Go 的高性能消息流处理框架，使用自定义 TCP 协议进行通信。

## 特性

- ✅ **模块化架构**：Server、Connection、Process、Reader 清晰分层
- ✅ **HeaderTcp 协议**：24 字节固定头 + 可变负载，支持多种消息类型
- ✅ **连接管理**：内置连接池，支持连接生命周期钩子
- ✅ **TCP 长连接**：支持 KeepAlive、优雅关闭、超时检测
- ✅ **扩展性强**：Process 接口可实现任意业务逻辑
- ✅ **生产就绪**：完善的测试覆盖和错误处理

## 项目结构

```
MyFlowHub-Core/
├── internal/core/           # 核心框架代码
│   ├── iface.go            # 核心接口定义
│   ├── config/             # 配置管理
│   ├── connmgr/            # 连接管理器
│   ├── header/             # HeaderTcp 协议
│   ├── listener/           # 监听器实现
│   │   └── tcp_listener/   # TCP 监听器
│   ├── process/            # 消息处理器
│   ├── reader/             # 数据读取器
│   └── server/             # 服务器编排
├── demo/                   # 示例程序
│   ├── server/             # 服务端 demo
│   └── client/             # 客户端 demo
└── tests/                  # 测试代码
```

## 快速开始

### 安装

```bash
go get github.com/yourname/MyFlowHub-Core
```

### 运行 Demo

#### 1. 启动服务端

```bash
cd demo/server
go run main.go
```

或者编译后运行：

```bash
go build -o demo_server.exe ./demo/server
./demo_server.exe
```

#### 2. 启动客户端

在另一个终端：

```bash
cd demo/client
go run main.go -n 5 -i 2
```

参数说明：
- `-n`：发送消息数量（0=无限）
- `-i`：发送间隔（秒）

### 示例代码

#### 创建服务器

```go
package main

import (
    "context"
    "time"
    
    "MyFlowHub-Core/internal/core/config"
    "MyFlowHub-Core/internal/core/connmgr"
    "MyFlowHub-Core/internal/core/header"
    "MyFlowHub-Core/internal/core/listener/tcp_listener"
    "MyFlowHub-Core/internal/core/process"
    "MyFlowHub-Core/internal/core/server"
)

func main() {
    // 创建配置
    cfg := config.NewMap(map[string]string{
        "addr": ":9000",
    })
    
    // 创建连接管理器
    cm := connmgr.New()
    
    // 创建处理器
    proc := process.NewSimple(nil)
    
    // 创建监听器
    listener := tcp_listener.New(":9000", tcp_listener.Options{
        KeepAlive:       true,
        KeepAlivePeriod: 30 * time.Second,
    })
    
    // 创建编解码器
    codec := header.HeaderTcpCodec{}
    
    // 创建服务器
    srv, _ := server.New(server.Options{
        Name:     "MyServer",
        Process:  proc,
        Codec:    codec,
        Listener: listener,
        Config:   cfg,
        Manager:  cm,
    })
    
    // 启动服务
    ctx := context.Background()
    srv.Start(ctx)
}
```

#### 自定义处理器

```go
type EchoProcess struct {
    logger *slog.Logger
}

func (p *EchoProcess) OnListen(conn core.IConnection) {
    p.logger.Info("新连接", "id", conn.ID())
}

func (p *EchoProcess) OnReceive(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) {
    // 处理收到的消息
    p.logger.Info("收到消息", "payload", string(payload))
    
    // 构造响应
    h := hdr.(header.HeaderTcp)
    respHeader := header.HeaderTcp{
        MsgID:  h.MsgID,
        Source: h.Target,
        Target: h.Source,
    }
    respPayload := []byte("Echo: " + string(payload))
    
    // 发送响应
    codec := header.HeaderTcpCodec{}
    conn.SendWithHeader(respHeader, respPayload, codec)
}

func (p *EchoProcess) OnSend(ctx context.Context, conn core.IConnection, hdr header.IHeader, payload []byte) error {
    return nil
}

func (p *EchoProcess) OnClose(conn core.IConnection) {
    p.logger.Info("连接关闭", "id", conn.ID())
}
```

## HeaderTcp 协议

### 帧格式

```
+--------+--------+--------+--------+--------+--------+
| TypeFmt| Flags  |      MsgID (4 bytes)      |
+--------+--------+--------+--------+--------+--------+
|      Source (4 bytes)    |    Target (4 bytes)     |
+--------+--------+--------+--------+--------+--------+
|    Timestamp (4 bytes)   |  PayloadLen (4 bytes)   |
+--------+--------+--------+--------+--------+--------+
|   Reserved (2 bytes)     |     Payload (N bytes)   |
+--------+--------+--------+-------------------------+
```

**字段说明：**

| 字段 | 大小 | 说明 |
|------|------|------|
| TypeFmt | 1B | 类型格式（bit 0-1: 消息大类，bit 2-7: 子协议） |
| Flags | 1B | 标志位（压缩、优先级等） |
| MsgID | 4B | 消息序列号 |
| Source | 4B | 发送方节点 ID |
| Target | 4B | 目标节点 ID |
| Timestamp | 4B | UTC 时间戳（秒） |
| PayloadLen | 4B | 负载长度 |
| Reserved | 2B | 保留字段 |
| Payload | N | 消息内容 |

### 消息大类

| 值 | 常量 | 说明 |
|----|------|------|
| 0 | MajorOKResp | 成功响应 |
| 1 | MajorErrResp | 错误响应 |
| 2 | MajorMsg | 普通消息 |
| 3 | MajorCmd | 命令消息 |

### 使用示例

```go
// 构造消息
hdr := header.HeaderTcp{
    MsgID:      1,
    Source:     0x12345678,
    Target:     0x9ABCDEF0,
    Timestamp:  uint32(time.Now().Unix()),
    PayloadLen: uint32(len(payload)),
}
hdr.WithMajor(header.MajorMsg).WithSubProto(1)

// 编码
codec := header.HeaderTcpCodec{}
frame, _ := codec.Encode(hdr, payload)

// 发送
conn.Write(frame)

// 解码
h, payload, _ := codec.Decode(conn)
```

## 测试

运行所有测试：

```bash
go test ./... -v
```

运行带覆盖率的测试：

```bash
go test ./... -cover
```

运行集成测试：

```bash
go test ./tests -v
```

## 架构设计

### 核心组件

1. **IServer**：服务器编排器
   - 管理监听器生命周期
   - 协调连接管理器和处理器
   - 提供优雅启停

2. **IConnectionManager**：连接管理器
   - 维护所有活跃连接
   - 支持连接查询、遍历
   - 提供连接钩子

3. **IProcess**：消息处理器
   - OnListen：连接建立时触发
   - OnReceive：收到消息时触发
   - OnSend：发送消息前触发
   - OnClose：连接关闭时触发

4. **IConnection**：连接抽象
   - 封装底层 TCP 连接
   - 提供元数据存储
   - 支持 header 编解码发送

5. **IReader**：读取器
   - 持续从连接读取数据
   - 使用 codec 解析帧
   - 触发接收事件

6. **IHeaderCodec**：编解码器
   - Encode：header + payload → frame
   - Decode：frame → header + payload

### 数据流

```
Client → Listener → Connection → Reader → Codec → Process.OnReceive
                                                        ↓
Server.Send → Process.OnSend → Codec → Connection → Client
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| DEMO_ADDR | :9000 | 服务端地址 |
| LOG_LEVEL | INFO | 日志级别 (DEBUG/INFO/WARN/ERROR) |
| LOG_JSON | false | 是否使用 JSON 格式 |
| LOG_CALLER | false | 是否显示调用位置 |

## 性能优化

- ✅ 使用 sync.RWMutex 保护并发读写
- ✅ 连接池避免频繁创建销毁
- ✅ 零拷贝：直接在 net.Conn 上操作
- ✅ 批量操作：Range 遍历使用快照避免长时间锁定

## 最佳实践

1. **自定义 Process**：根据业务需求实现 IProcess 接口
2. **子协议规划**：使用 SubProto 区分不同消息类型
3. **错误处理**：在 Process 中捕获并处理业务异常
4. **优雅关闭**：使用 context 控制服务生命周期
5. **日志记录**：在关键节点记录日志便于排查

## 常见问题

### Q: 如何支持多种协议？

实现不同的 `IHeaderCodec` 和 `IListener`，在创建 Server 时传入即可。

### Q: 如何实现消息路由？

在 `Process.OnReceive` 中根据 header 的 `Target` 字段查找目标连接并转发。

### Q: 如何实现认证？

在 `Process.OnListen` 中实现握手逻辑，验证失败时关闭连接。

### Q: 如何处理慢客户端？

设置写超时，超时后主动关闭连接：
```go
conn.RawConn().SetWriteDeadline(time.Now().Add(5 * time.Second))
```

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License

## 联系方式

- 作者：[Your Name]
- Email：[your.email@example.com]
- GitHub：[https://github.com/yourname/MyFlowHub-Core](https://github.com/yourname/MyFlowHub-Core)

---

**注意**：本项目仍在积极开发中，API 可能会有变化。生产环境使用前请充分测试。

