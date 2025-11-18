# MyFlowHub-Core Demo

本目录包含使用 MyFlowHub-Core 框架的示例程序。

## 架构说明

Demo 使用了完整的 MyFlowHub-Core 框架栈：

- **HeaderTcp 协议**：24 字节固定头 + 可变长度 payload
- **Server 框架**：listener、connection manager、process 三层架构
- **Process Dispatcher**：基于子协议的多通道调度，支持配置 channel/worker 数量
- **TCP 长连接**：支持 KeepAlive、优雅关闭
- **多子协议演示**：子协议 1 回显、子协议 2 转大写

## 快速开始

### 1. 编译

```powershell
# 编译服务端
go build -o demo_server.exe ./demo/server

# 编译客户端
go build -o demo_client.exe ./demo/client
```

### 2. 启动服务端

```powershell
# 使用默认端口 :9000
./demo_server.exe

# 指定端口与调度配置
env DEMO_ADDR=:8080 DEMO_PROC_CHANNELS=4 DEMO_PROC_WORKERS=2 ./demo_server.exe

# 启用 DEBUG 日志
LOG_LEVEL=DEBUG ./demo_server.exe
```

### 3. 启动客户端

```powershell
# 使用默认配置（连接 127.0.0.1:9000，发送 5 条消息，间隔 3 秒）
./demo_client.exe

# 指定消息数量和间隔
./demo_client.exe -n 10 -i 2

# 无限发送消息（n=0）
./demo_client.exe -n 0 -i 5
```

客户端会交替发送子协议 1（回显）与子协议 2（转大写），方便观察多处理器调度效果。

## 参数说明

### 服务端

通过环境变量配置：

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| DEMO_ADDR | :9000 | 监听地址 |
| DEMO_PROC_CHANNELS | 2 | Dispatcher channel 数量 |
| DEMO_PROC_WORKERS | 2 | 每个 channel 的 worker 数 |
| DEMO_PROC_BUFFER | 128 | channel 缓冲长度 |
| LOG_LEVEL | INFO | 日志级别：DEBUG/INFO/WARN/ERROR |
| LOG_JSON | false | 是否使用 JSON 格式日志 |
| LOG_CALLER | false | 是否显示调用位置 |

### 客户端

命令行参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| -i | 3 | 消息发送间隔（秒） |
| -n | 5 | 发送消息数量（0=无限） |

环境变量：

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| DEMO_ADDR | :9000 | 服务端地址 |
| LOG_LEVEL | INFO | 日志级别 |

## 协议格式

### HeaderTcp 结构（24 字节）

```
+--------+--------+--------+--------+--------+--------+
| TypeFmt| Flags  |      MsgID (4 bytes)      |
+--------+--------+--------+--------+--------+--------+
|      Source (4 bytes)    |    Target (4 bytes)     |
+--------+--------+--------+--------+--------+--------+
|    Timestamp (4 bytes)   |  PayloadLen (4 bytes)   |
+--------+--------+--------+--------+--------+--------+
|   Reserved (2 bytes)     |
+--------+--------+--------+
```

**字段说明：**

- **TypeFmt**：类型格式字节
  - bit 0-1：消息大类（0=OK_RESP, 1=ERR_RESP, 2=MSG, 3=CMD）
  - bit 2-7：子协议号（0-63）
- **Flags**：标志位（压缩、优先级等）
- **MsgID**：消息序列号，用于请求-响应关联
- **Source**：发送方节点 ID
- **Target**：目标节点 ID
- **Timestamp**：UTC 时间戳（秒）
- **PayloadLen**：负载长度
- **Reserved**：保留字段

### 消息流程

1. **客户端 → 服务端**：
   - 子协议 1：Major=MSG, SubProto=1，payload 为普通字符串（服务器回显）
   - 子协议 2：Major=MSG, SubProto=2，payload 为普通字符串（服务器返回大写版本）

2. **服务端 → 客户端**：
   - 子协议 1：Major=OK_RESP, SubProto=1，payload="ECHO: ..."
   - 子协议 2：Major=OK_RESP, SubProto=2，payload="UPPER(n): ..."

## 示例输出

### 服务端

```
time=2025-11-16T10:00:05+08:00 level=INFO msg="服务端启动" listen=:9000
time=2025-11-16T10:00:05+08:00 level=INFO msg="Process pipeline ready" channels=2 workers_per_channel=2 channel_buffer=128
time=2025-11-16T10:00:05+08:00 level=INFO msg="EchoHandler" conn="[::]:9000->[::1]:54321" payload="Hello from client, msg #0"
time=2025-11-16T10:00:08+08:00 level=INFO msg="UpperHandler" conn="[::]:9000->[::1]:54321" payload="Hello from client, msg #1" resp="UPPER(2): HELLO FROM CLIENT, MSG #1"
```

### 客户端

```
time=2025-11-16T10:00:05+08:00 level=INFO msg="已发送" msgid=1 subproto=1 payload="Hello from client, msg #0"
time=2025-11-16T10:00:05+08:00 level=INFO msg="收到响应" major=0 subproto=1 msgid=1 payload="ECHO: Hello from client, msg #0"
time=2025-11-16T10:00:08+08:00 level=INFO msg="已发送" msgid=2 subproto=2 payload="Hello from client, msg #1"
time=2025-11-16T10:00:08+08:00 level=INFO msg="收到响应" major=0 subproto=2 msgid=2 payload="UPPER(2): HELLO FROM CLIENT, MSG #1"
```

其余章节保持不变，可根据需要扩展。
