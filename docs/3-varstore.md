VarStore 协议（SubProto=3：set/get/notify）
==========================================

变量模型
--------
- 键 = `owner_node_id:name`，name 大小写敏感，字符集仅字母/数字/下划线。
- 可见性：`public` 公开可读（可被他人更新且会通知 owner）；`private` 仅 owner 可读/改。
- type：随 `set` 携带，空则默认 `"string"`。

动作（JSON：`{"action": "...", "data": {...}}`）
--------------------------------------------
- `set` / `assist_set`：创建/更新（Upsert）。只有当 `owner==SourceID` 时才允许新建；已有变量时按可见性规则更新。
  ```json
  { "name": "temp_1", "value": "22.5", "visibility": "public", "type": "string" }
  ```
  resp (`set_resp`):
  ```json
  { "code": 1, "msg": "ok", "name": "temp_1", "owner": 5, "visibility": "public", "type": "string" }
  ```
- `get` / `assist_get`：读取。
  ```json
  { "name": "temp_1" }
  ```
  resp (`get_resp` / `assist_get_resp`):
  ```json
  { "code": 1, "msg": "ok", "name": "temp_1", "value": "22.5", "owner": 5, "visibility": "public", "type": "string" }
  ```
  失败示例：`{ "code": 404, "msg": "not found" }`，`{ "code": 403, "msg": "forbidden" }`
- `notify_update`：他人修改时通知所有者，data 同 `get_resp`（含 name/value/owner/type）。

处理规则
--------
- 新建：仅当 `owner == SourceID` 且未缓存该键时允许创建。
- 自己 set（SourceID=owner）：本地写缓存后向父发 `assist_set`，父再向上逐级缓存。
- 他人 set（SourceID≠owner）：
  - 每级父节点检查是否有子孙是 owner；有则向子孙发 `notify_update`（逐跳下行刷新/告知），之后沿父链继续发 `assist_set` 以缓存。
  - `set` 与 `assist_set` 的区别仅在于是否已下行通知 owner（可用 `notified` 标记）。
- get：本地命中且可访问（公开或 owner==请求方）直接返回；未命中向父发 `assist_get`，收到 `assist_get_resp` 后缓存并回复；最顶层未命中返回 404。上行保留原始 `SourceID`，响应 `TargetID` 指向原始请求方。
- 类型：`set` 可带 type；为空则保留原类型或默认 `"string"`；`get/resp` 返回当前类型。

报文/头部建议
-------------
- `get_resp`、`notify_update` 建议用 `MajorCmd`，确保逐跳解包/缓存；`set_resp` 不强制 Cmd（可用 OKResp）。
- 转发时保留原始 `SourceID`，仅调整 `TargetID`（父或 0）与 `Major/SubProto`。

示例帧 payload
--------------
- set：
```
{"action":"set","data":{"name":"sensor_a","value":"22.5","visibility":"public","type":"string"}}
```
- set_resp：
```
{"action":"set_resp","data":{"code":1,"msg":"ok","name":"sensor_a","owner":5,"visibility":"public","type":"string"}}
```
- get：
```
{"action":"get","data":{"name":"sensor_a"}}
```
- get_resp 命中：
```
{"action":"get_resp","data":{"code":1,"msg":"ok","name":"sensor_a","value":"22.5","owner":5,"visibility":"public","type":"string"}}
```
- 未命中：
```
{"action":"get_resp","data":{"code":404,"msg":"not found"}}
```

集成
----
- Dispatcher 注册：`dp.RegisterHandler(handler.NewVarStoreHandler(logger))`
- SubProto=3，Major 可用 Msg/Cmd；handler 需在每跳解包后自行决定上行/下行。

注意
----
- 当前仅内存缓存，重启即丢失；可按需接入持久化。
- 公开变量允许他人更新并通知 owner，未来可按需收紧为更严格 ACL。***
