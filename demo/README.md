# MyFlowHub-Core Demo

本目录包含使用 MyFlowHub-Core 框架的示例程序。

## 架构说明

Demo 使用了完整的 MyFlowHub-Core 框架栈：

- **HeaderTcp 协议**：24 字节固定头 + 可变长度 payload
- **Server 框架**：listener、connection manager、process 三层架构
- **TCP 长连接**：支持 KeepAlive、优雅关闭
- **消息回显**：服务端收到消息后原样返回（前缀 "ECHO: "）

## 快速开始

### 1. 编译

```bash
# 编译服务端
go build -o demo_server.exe ./demo/server

# 编译客户端
go build -o demo_client.exe ./demo/client
```

### 2. 启动服务端

```bash
# 使用默认端口 :9000
./demo_server.exe

# 或指定端口
DEMO_ADDR=:8080 ./demo_server.exe

# 启用 DEBUG 日志
LOG_LEVEL=DEBUG ./demo_server.exe
```

### 3. 启动客户端

在另一个终端窗口：

```bash
# 使用默认配置（连接 127.0.0.1:9000，发送 5 条消息，间隔 3 秒）
./demo_client.exe

# 指定消息数量和间隔
./demo_client.exe -n 10 -i 2

# 无限发送消息（n=0）
./demo_client.exe -n 0 -i 5

# 连接到其他地址
DEMO_ADDR=:8080 ./demo_client.exe
```

## 参数说明

### 服务端

通过环境变量配置：

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| DEMO_ADDR | :9000 | 监听地址 |
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
   ```
   Header: Major=MSG, SubProto=1, MsgID=1, ...
   Payload: "Hello from client, msg #0"
   ```

2. **服务端 → 客户端**：
   ```
   Header: Major=OK_RESP, SubProto=1, MsgID=1, ...
   Payload: "ECHO: Hello from client, msg #0"
   ```

## 示例输出

### 服务端

```
time=2025-11-16T10:00:00+08:00 level=INFO msg="服务端启动" listen=:9000
time=2025-11-16T10:00:00+08:00 level=INFO msg="tcp listener started" addr="[::]:9000"
time=2025-11-16T10:00:00+08:00 level=INFO msg="服务器已启动" addr="[::]:9000"
time=2025-11-16T10:00:05+08:00 level=INFO msg="新连接建立" id="[::]:9000->[::1]:54321" remote="[::1]:54321"
time=2025-11-16T10:00:05+08:00 level=INFO msg="收到消息" conn="[::]:9000->[::1]:54321" payload_len=28 payload="Hello from client, msg #0"
```

### 客户端

```
time=2025-11-16T10:00:05+08:00 level=INFO msg="开始连接" addr="127.0.0.1:9000"
time=2025-11-16T10:00:05+08:00 level=INFO msg="连接成功" local="127.0.0.1:54321" remote="127.0.0.1:9000"
time=2025-11-16T10:00:05+08:00 level=INFO msg="已发送" msgid=1 payload="Hello from client, msg #0"
time=2025-11-16T10:00:05+08:00 level=INFO msg="收到响应" major=0 subproto=1 msgid=1 source=0x01020304 target=0x00A1B2C3 payload="ECHO: Hello from client, msg #0"
```

## 优雅退出

服务端支持通过 `Ctrl+C` 或 `SIGTERM` 信号触发优雅关闭：

1. 停止接受新连接
2. 等待所有现有连接的读循环退出
3. 关闭所有连接
4. 清理资源

客户端发送完指定数量的消息后会自动退出。

## 故障排查

### 端口已被占用

```
Error: listen tcp :9000: bind: address already in use
```

**解决方法**：使用不同的端口
```bash
DEMO_ADDR=:9001 ./demo_server.exe
```

### 连接被拒绝

```
Error: dial tcp 127.0.0.1:9000: connect: connection refused
```

**解决方法**：确保服务端已启动

### 读取/写入超时

检查网络连接和防火墙设置。

## 扩展开发

基于此 demo，你可以：

1. **实现自定义 Process**：修改 `EchoProcess` 以支持更复杂的业务逻辑
2. **添加子协议**：使用不同的 `SubProto` 值实现多种消息类型
3. **支持多客户端**：服务端自动支持多连接并发处理
4. **添加认证**：在 `OnListen` 中实现握手和鉴权
5. **消息路由**：根据 `Target` 字段实现消息转发

## 许可证

本项目采用与 MyFlowHub-Core 相同的许可证。

