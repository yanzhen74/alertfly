# AlertFly 报警对接指南

本文档面向需要向 AlertFly 发送报警消息的外部系统（监控平台、CI/CD 流水线、业务系统等），说明消息接收接口、格式规范及命名约定，供对接开发参考。

---

## 一、概述

AlertFly 是消息的**消费端**，从消息通道接收报警后以桌面弹窗形式提醒用户。外部系统可通过以下四种方式之一发送报警：

| 通道 | 协议 | 模式 | 适用场景 | 部署难度 |
|------|------|------|----------|----------|
| **HTTP Webhook** | HTTP POST | 请求-响应 | 外部系统无 Redis 客户端 / 跨网段推送 | ⭐ 最简单 |
| Redis PubSub | Redis PUBLISH | 实时发布/订阅 | 即时报警，无需消息持久化 | ⭐⭐ |
| Redis Stream | Redis XADD | 消费者组消费 | 需要消息持久化和可靠投递 | ⭐⭐ |
| Kafka | Producer 写入 | Consumer Group 消费 | 高吞吐、需要消息回溯 | ⭐⭐⭐ |

**推荐选择顺序：**

1. **优先使用 HTTP Webhook**：只需能发 HTTP 请求即可，无需 Redis/Kafka 客户端库，无需处理连接管理
2. 已有 Redis 基础设施 → **Redis PubSub**（简单）或 **Redis Stream**（可靠）
3. 高吞吐场景或多消费者 → **Kafka**

---

## 二、消息格式

### 2.1 统一消息结构

无论使用哪种通道，消息体均为 **JSON 格式**：

```json
{
  "title": "CPU 使用率超过阈值",
  "level": "error",
  "mission": "infra-monitor",
  "sender": "prometheus",
  "subtype": "cpu_alert",
  "content": "CPU 利用率持续超过 90%，已持续 5 分钟"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `title` | string | **是**（或与 content 至少填一个） | 报警标题，弹窗中显示的主标题 |
| `content` | string | 否 | 报警详情内容，支持自定义 JSON 字符串或纯文本 |
| `level` | string | 推荐填写 | 报警级别，可选值：`info` / `warn` / `error` / `critical` |
| `mission` | string | 推荐填写 | 任务/项目名称（如 `deploy`、`infra-monitor`） |
| `sender` | string | 推荐填写 | 发送者标识（如 `prometheus`、`jenkins`、`zabbix`） |
| `subtype` | string | 推荐填写 | 消息子类型（如 `cpu_alert`、`disk_alert`、`deploy`） |
| `source` | string | 否 | 消息来源标识，通常由 AlertFly 自动填充 |
| `topic` | string | 否 | Topic/Channel 名称，通常由 AlertFly 自动填充 |

> **说明：** `level`、`mission`、`sender`、`subtype` 虽然非必填，但如果 JSON 中未提供，AlertFly 会尝试从 Topic/Channel 名称中自动解析（见第三章命名规范）。因此建议**尽量在 JSON 中明确填写这些字段**。

### 2.2 消息解析优先级

1. **JSON 字段优先**：JSON 消息中已包含的字段以 JSON 值为准
2. **Topic/Channel 名称补充**：JSON 中缺失的字段从 Topic/Channel 名称中自动提取
3. **JSON 解析失败降级**：非法 JSON 时，原始文本作为 `content`，标题标记为 `Raw Message`

---

## 三、Topic / Channel 命名规范

AlertFly 支持通过 Topic/Channel 名称自动提取消息元数据，实现灵活的模式匹配订阅。

### 3.1 Redis Channel 命名（冒号分隔）

```
alert:{mission}:{sender}:{subtype}:{level}
```

| 段 | 说明 | 示例 |
|---|---|---|
| mission | 业务任务名称 | deploy、backup、monitor |
| sender | 系统/服务来源 | crm、erp、gateway、jenkins |
| subtype | 具体报警分类 | cpu-high、disk-full、oom、timeout |
| level | 严重程度 | critical、error、warning、info |

**完整示例：**

```
alert:deploy:crm:timeout:error
alert:backup:erp:disk-full:critical
alert:monitor:gateway:cpu-high:warning
```

**PSUBSCRIBE 订阅灵活性：**

| 订阅模式 | 含义 |
|----------|------|
| `alert:*` | 接收所有报警（最简单） |
| `alert:*:*:*:*` | 接收所有报警（明确四段） |
| `alert:taska:*:*:*` | 接收 taska 任务的所有报警 |
| `alert:*:crm:*:critical` | 接收 CRM 系统的所有严重报警 |
| `alert:taska:crm:cpu-high:*` | 接收 taska+CRM+CPU过高的所有级别 |
| `alert:*:*:*:critical` | 接收所有系统的严重报警 |
| `alert:*:*:cpu-high:*` | 接收所有系统的 CPU 过高报警 |

> AlertFly Redis 消费者检测到 channel 配置中包含通配符（`*`、`?`、`[`）时，自动切换为 `PSUBSCRIBE` 模式订阅；否则使用 `SUBSCRIBE` 精确订阅。

### 3.2 Kafka Topic 命名（点号分隔）

```
alert.{mission}.{sender}.{subtype}.{level}
```

与 Redis 话题规范的对应关系：

| 段 | Redis 格式 | Kafka 格式 |
|---|---|---|
| 分隔符 | `:` | `.` |
| 完整格式 | `alert:{mission}:{sender}:{subtype}:{level}` | `alert.{mission}.{sender}.{subtype}.{level}` |

**完整示例：**

```
alert.deploy.crm.timeout.error
alert.backup.erp.disk-full.critical
alert.monitor.gateway.cpu-high.warning
```

**正则订阅示例：**

Kafka 消费者支持在 topics 列表中使用 `regex:` 前缀（Go RE2 语法），配合 `!regex:` 排除规则：

| 配置 | 匹配效果 |
|------|-----------|
| `alert-events` | 精确匹配 |
| `regex:alert\..*` | 所有 alert 话题 |
| `regex:alert\.deploy\..*` | 所有部署任务报警 |
| `regex:alert\..*\.crm\..*\.(critical\|error)` | CRM 系统的 critical 和 error 报警 |
| `regex:alert\..*\..*\.cpu-high\..*` | 所有 CPU 过高报警 |
| `!regex:alert\.deploy\..*` | 排除所有 deploy 任务（配合包含规则使用） |
| `!regex:alert\..*\..*\..*\.warning` | 排除所有 warning 级别 |

> 正则中 `.` 需要转义为 `\.`（否则匹配任意字符）
> 正则匹配定期执行（默认 30 秒扫描，可通过 `kafka.topic_scan_interval` 配置）
> 精确 topic 和正则 topic 可以混用；精确指定的 topic 不受排除规则影响

### 3.3 命名规范不是强制的

Topic/Channel 命名规范是**可选的辅助手段**。如果系统不使用这种命名格式，只需在 JSON 消息体中明确填写 `mission`、`sender`、`subtype`、`level` 字段即可，AlertFly 会自动适配。

---

## 四、对接方式详解

### 4.1 HTTP Webhook（推荐新接入方式）

**适用场景：**

- 外部系统无法或不便安装 Redis/Kafka 客户端库
- 跨网段推送（例如生产网 → 办公网的 AlertFly）
- 简单的 shell 脚本 / 命令行工具触发报警
- 需要统一鉴权和审计入口

**架构：**

```
外部系统 (Jenkins/监控/脚本)
    │  HTTP POST JSON
    ▼
Python Webhook 网关 (update-server/start_server.py, 0.0.0.0:8000)
    │  Redis PUBLISH alert:{mission}:{sender}:{subtype}:{level}
    ▼
Redis PubSub
    │  PSUBSCRIBE alert:*
    ▼
AlertFly 客户端 → 存储 + 弹窗通知
```

Webhook 网关与 AlertFly 自更新服务共用同一个 Python 服务，部署位置详见 [update-server/README.md](../update-server/README.md)。

#### 4.1.1 网关部署

```bash
cd update-server
# 使用默认配置（Redis localhost:6379）
python3 start_server.py

# 指定 Redis 地址 + 启用 Token 鉴权
python3 start_server.py \
  --redis-host 10.0.0.5 \
  --redis-port 6379 \
  --redis-password yourpass \
  --token mysecret \
  --port 8000
```

**命令行参数：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--port` | 8000 | HTTP 监听端口 |
| `--redis-host` | localhost | Redis 服务地址 |
| `--redis-port` | 6379 | Redis 端口 |
| `--redis-password` | 空 | Redis 密码 |
| `--redis-db` | 0 | Redis 数据库编号 |
| `--token` | 空 | Webhook 鉴权 Token（空=不校验） |
| `--webhook-path` | /api/webhook | Webhook 路由路径 |

服务启动后监听 `0.0.0.0:{port}`，内网可访问。特性：

- **零第三方依赖**：仅使用 Python 3 标准库（Redis 通信用原生 socket 实现 RESP 协议）
- **多线程并发**：基于 `ThreadingTCPServer`，支持并发请求
- **断线重连**：Redis 连接异常自动重连一次
- **同时提供**：静态文件服务（自更新用）+ Webhook API

#### 4.1.2 API 接口规范

**端点：** `POST http://{host}:{port}/api/webhook`

**请求头：**

| Header | 必填 | 说明 |
|--------|------|------|
| `Content-Type` | 是 | `application/json` |
| `X-Webhook-Token` | 视配置 | 网关启用 `--token` 时必填；也可通过 `?token=xxx` 查询参数传递 |

**请求体字段：**

同 [2.1 统一消息结构](#21-统一消息结构)。字段默认值如下（当请求体未提供时）：

| 字段 | 默认值 |
|------|--------|
| `level` | `warn` |
| `mission` | `general` |
| `sender` | `webhook` |
| `subtype` | `alert` |
| `source` | `webhook`（自动填充） |

**响应格式：**

成功：

```json
{
  "code": 0,
  "msg": "ok",
  "channel": "alert:deploy:jenkins:build:error",
  "subscribers": 2
}
```

`subscribers` 表示 Redis 中订阅了该 channel 的客户端数量（含 AlertFly 实例）。

失败：

| HTTP 状态码 | code | msg | 场景 |
|-------------|------|-----|------|
| 400 | 1 | `请求体为空` / `JSON 解析失败: ...` / `消息必须包含 title 或 content 字段` | 请求格式错误 |
| 401 | 1 | `Token 验证失败` | Token 未通过 |
| 502 | 1 | `Redis 发布失败: ...` | Redis 连接或 PUBLISH 异常 |

**辅助端点：**

- `GET /api/webhook` — 返回 API 使用说明（JSON 格式），可用于探活

#### 4.1.3 调用示例

**curl：**

```bash
curl -X POST http://172.15.9.22:8000/api/webhook \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Token: mysecret" \
  -d '{
    "title": "部署失败",
    "content": "Pipeline #1234 构建失败，请检查日志",
    "level": "error",
    "mission": "deploy",
    "sender": "jenkins",
    "subtype": "build"
  }'
```

**Python（requests）：**

```python
import requests

resp = requests.post(
    "http://172.15.9.22:8000/api/webhook",
    headers={"X-Webhook-Token": "mysecret"},
    json={
        "title": "订单服务异常",
        "content": "订单创建接口 P99 = 8.2s，超过 5s 阈值",
        "level": "error",
        "mission": "order-service",
        "sender": "apm-monitor",
        "subtype": "latency_alert",
    },
    timeout=5,
)
resp.raise_for_status()
print(resp.json())
```

**Shell（无 curl 时用 wget）：**

```bash
wget -qO- --header="Content-Type: application/json" \
     --header="X-Webhook-Token: mysecret" \
     --post-data='{"title":"磁盘告警","content":"/dev/sda1 95%","level":"warn","mission":"disk-monitor","sender":"node-exporter","subtype":"disk_alert"}' \
     http://172.15.9.22:8000/api/webhook
```

**Java（OkHttp）：**

```java
OkHttpClient client = new OkHttpClient();
String json = "{\"title\":\"服务宕机\",\"level\":\"critical\",\"mission\":\"api\",\"sender\":\"healthcheck\",\"subtype\":\"down\",\"content\":\"API 节点 10.0.0.5 无响应\"}";

Request req = new Request.Builder()
    .url("http://172.15.9.22:8000/api/webhook")
    .header("X-Webhook-Token", "mysecret")
    .post(RequestBody.create(json, MediaType.parse("application/json")))
    .build();

try (Response resp = client.newCall(req).execute()) {
    System.out.println(resp.body().string());
}
```

**Node.js（fetch）：**

```javascript
await fetch("http://172.15.9.22:8000/api/webhook", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "X-Webhook-Token": "mysecret",
  },
  body: JSON.stringify({
    title: "构建失败",
    content: "test suite failed",
    level: "error",
    mission: "ci",
    sender: "jenkins",
    subtype: "test",
  }),
});
```

#### 4.1.4 与 Redis 直连的对比

| 维度 | HTTP Webhook | Redis 直连 |
|------|-------------|-----------|
| 客户端依赖 | 无（任何 HTTP 库） | Redis 客户端库 |
| 网络要求 | 能访问网关 HTTP 端口 | 能访问 Redis 端口 |
| 鉴权 | Token 头（可选） | Redis 密码 |
| 连接管理 | 无状态 HTTP | 长连接需管理 |
| 投递确认 | HTTP 响应立即返回 subscribers 数量 | PUBLISH 返回 subscribers 数量 |
| 消息持久化 | 依赖 Redis（PubSub 不持久化） | 同左 |
| 审计日志 | 网关侧集中打印 | 分散在各发送端 |

---

### 4.2 Redis PubSub 模式

**对接方式：** 外部系统使用 Redis 客户端执行 `PUBLISH` 命令。

**参数：**
- Channel：按命名规范填写，或使用自定义名称（如 `alerts`）
- Message：JSON 格式的消息体

**示例（redis-cli）：**

```bash
redis-cli PUBLISH "alert:deploy:jenkins:build:error" \
  '{"title":"部署失败","level":"error","mission":"deploy","sender":"jenkins","subtype":"build","content":"Pipeline #1234 构建失败"}'
```

**示例（Python）：**

```python
import redis
import json

r = redis.Redis(host='localhost', port=6379, db=0)

message = {
    "title": "部署失败",
    "level": "error",
    "mission": "deploy",
    "sender": "jenkins",
    "subtype": "build",
    "content": "Pipeline #1234 构建失败"
}

r.publish("alert:deploy:jenkins:build:error", json.dumps(message, ensure_ascii=False))
```

**特点：**
- 实时性最高，消息发出即送达
- 消息不持久化，离线期间消息丢失
- 支持 PSUBSCRIBE 模式匹配订阅

---

### 4.3 Redis Stream 模式

**对接方式：** 外部系统使用 Redis 客户端执行 `XADD` 命令。

**参数：**
- Stream 名称：与 AlertFly 配置中的 `redis.stream` 一致（默认 `alert_stream`）
- Fields：将消息各字段作为 Stream Entry 的 key-value 对写入

**示例（redis-cli）：**

```bash
redis-cli XADD alert_stream '*' \
  title "磁盘空间不足" \
  level "warn" \
  mission "disk-monitor" \
  sender "node-exporter" \
  subtype "disk_alert" \
  content "/data 分区可用空间低于 15%"
```

**示例（Python）：**

```python
r.xadd("alert_stream", {
    "title": "磁盘空间不足",
    "level": "warn",
    "mission": "disk-monitor",
    "sender": "node-exporter",
    "subtype": "disk_alert",
    "content": "/data 分区可用空间低于 15%"
})
```

**特点：**
- 消息持久化，支持消费者组和 ACK 机制
- 离线后恢复连接可继续消费
- 适合对消息可靠性要求较高的场景

---

### 4.4 Kafka 模式

**对接方式：** 外部系统作为 Kafka Producer，向指定 Topic 写入消息。

**参数：**
- Topic：按 `alert.{mission}.{sender}.{subtype}.{level}` 格式命名，或使用自定义名称
- Key（可选）：可用于消息分区
- Value：JSON 格式的消息体

**示例（Python kafka-python）：**

```python
from kafka import KafkaProducer
import json

producer = KafkaProducer(
    bootstrap_servers=['localhost:9092'],
    value_serializer=lambda v: v.encode('utf-8')
)

message = {
    "title": "内存使用率告警",
    "level": "error",
    "mission": "memory-monitor",
    "sender": "grafana",
    "subtype": "memory_alert",
    "content": "内存使用率已达 92% 且持续上升"
}

producer.send(
    "alert.memory-monitor.grafana.memory_alert.error",
    value=json.dumps(message, ensure_ascii=False)
)
producer.flush()
```

**示例（kafka-console-producer）：**

```bash
echo '{"title":"CRM部署超时","content":"部署超过30分钟","level":"error"}' | \
  kafka-console-producer --broker-list localhost:9092 --topic alert.deploy.crm.timeout.error
```

**特点：**
- 支持多 Topic 订阅和正则匹配订阅（详见 [3.2 Kafka Topic 命名](#32-kafka-topic-命名点号分隔)）
- 支持 Consumer Group，可多实例负载均衡
- 消息可回溯，offset 自动管理
- 适合高吞吐、多消费者场景

**Broker 版本兼容：**

AlertFly 通过 `kafka.version` 配置项指定协议版本（默认 `2.0.0`），支持 Kafka 0.10.0+。版本不匹配时可能连接失败，请显式配置为与 broker 一致的版本号：

```yaml
kafka:
  version: "1.0.0"  # 与实际 broker 版本匹配
```

---

## 五、报警级别定义

AlertFly 内部使用三级级别（`info` / `warn` / `error`），话题命名规范额外支持 `critical` 和 `warning` 别名：

| 级别 | 含义 | 建议用途 | 弹窗 |
|------|------|----------|------|
| `critical` | 紧急 | 生产环境重大故障，需立即处理 | ✅ |
| `error` | 严重错误 | 需要处理的生产故障、系统宕机 | ✅ |
| `warn` / `warning` | 警告 | 资源使用率超阈值、性能下降 | ✅ |
| `info` | 信息 | 部署完成、任务成功等一般性通知 | ✅ |

**声音报警：** 在 `config.yaml` 的 `notifier.sound_level` 中配置触发声音的最低级别（如 `warn` 表示 warn 及以上级别会响铃）。

---

## 六、过滤机制说明

AlertFly 支持基于以下三个维度过滤消息，控制哪些消息弹窗通知：

| 过滤维度 | 对应字段 | 说明 |
|----------|----------|------|
| 任务名 | `mission` | 按项目/任务过滤，如只接收 `deploy` 任务的报警 |
| 发送者 | `sender` | 按来源系统过滤，如只接收 `prometheus` 的报警 |
| 子类型 | `subtype` | 按消息类型过滤，如只接收 `cpu_alert` 类型的报警 |

**重要说明：**

- 过滤仅影响**弹窗通知**，所有消息无论是否匹配过滤条件，都会被**存储到本地数据库**
- 过滤条件为空表示接收所有（不过滤）
- 过滤匹配**忽略大小写**
- `source` 为 `system` 的内部消息（如自更新事件）始终弹窗，不受过滤影响

**对接建议：** 外部系统应尽量在消息中准确填写 `mission`、`sender`、`subtype` 字段，以便用户在 AlertFly 端进行精确过滤。

---

## 七、对接最佳实践

### 7.1 消息发送建议

1. **JSON 字段尽量完整**：只有 `title` 是必填，但完整字段有助于展示和分类
2. **content 支持自定义格式**：可以是纯文本，也可以是 JSON 字符串
3. **合理设置 level**：避免所有消息都用同一级别，正确的级别有助于用户区分紧急程度
4. **mission 和 sender 保持稳定**：同一系统发出的消息应使用固定值，便于用户配置过滤规则
5. **消息编码统一 UTF-8**

### 7.2 通道选择建议

| 场景 | 推荐通道 |
|------|---------|
| 简单脚本、CI/CD、跨网段 | HTTP Webhook |
| 已有 Redis 基础设施，即时报警 | Redis PubSub |
| 需要消息可靠投递 | Redis Stream |
| 高吞吐、多消费者、消息回溯 | Kafka |

### 7.3 连接建议

1. **HTTP Webhook**：网关应部署在内网可访问的机器上；生产环境建议启用 Token 鉴权
2. **Redis 连接**：确保 AlertFly 能访问 Redis 服务地址，注意 `protected-mode` 和防火墙配置
3. **Kafka 连接**：确保 `group_id` 与其他消费者不冲突；broker 版本需与配置一致

### 7.4 测试验证

对接完成后，可使用项目自带的测试脚本验证：

```bash
# Linux/macOS：通过 Redis 直连发送测试报警
./scripts/send_alert.sh "测试标题" "测试内容" warn

# Windows
scripts\send_alert.bat "测试标题" "测试内容" warn

# 通过 HTTP Webhook 测试
curl -X POST http://<网关IP>:8000/api/webhook \
  -H "Content-Type: application/json" \
  -d '{"title":"测试","content":"webhook 测试","level":"info"}'

# 或使用 Go Mock 工具批量发送
go run scripts/redis_mock_send.go -mode pubsub -channel alert:test:mock:manual:warn
```

---

## 八、完整对接示例

### 8.1 HTTP Webhook 方式（推荐）

```python
import requests

class AlertFlyWebhookSender:
    """AlertFly HTTP Webhook 报警发送器"""

    def __init__(self, gateway_url, token=None, timeout=5):
        self.url = gateway_url.rstrip("/") + "/api/webhook"
        self.timeout = timeout
        self.headers = {"Content-Type": "application/json"}
        if token:
            self.headers["X-Webhook-Token"] = token

    def send(self, title, content="", level="warn",
             mission="general", sender="webhook", subtype="alert"):
        """发送报警消息"""
        payload = {
            "title": title,
            "content": content,
            "level": level,
            "mission": mission,
            "sender": sender,
            "subtype": subtype,
        }
        resp = requests.post(self.url, json=payload,
                             headers=self.headers, timeout=self.timeout)
        resp.raise_for_status()
        return resp.json()


# 使用示例
sender = AlertFlyWebhookSender(
    gateway_url="http://172.15.9.22:8000",
    token="mysecret",
)

sender.send(
    title="订单服务异常",
    content="订单创建接口响应时间超过 5s，当前 P99 = 8.2s",
    level="error",
    mission="order-service",
    sender="apm-monitor",
    subtype="latency_alert",
)
```

### 8.2 Redis 直连方式

```python
import redis
import json

class AlertFlyRedisSender:
    """AlertFly Redis 直连报警发送器"""

    def __init__(self, redis_addr="localhost:6379", password="", db=0):
        host, port = redis_addr.split(":")
        self.client = redis.Redis(
            host=host, port=int(port), password=password, db=db
        )

    def send(self, title, content="", level="warn",
             mission="general", sender="", subtype="alert"):
        message = {
            "title": title,
            "level": level,
            "mission": mission,
            "sender": sender,
            "subtype": subtype,
            "content": content,
        }
        channel = f"alert:{mission}:{sender}:{subtype}:{level}"
        return self.client.publish(channel, json.dumps(message, ensure_ascii=False))


sender = AlertFlyRedisSender(redis_addr="192.168.1.100:6379")
sender.send(
    title="订单服务异常",
    content="订单创建接口 P99 = 8.2s",
    level="error",
    mission="order-service",
    sender="apm-monitor",
    subtype="latency_alert",
)
```

---

## 九、常见问题

**Q1: 消息发送后 AlertFly 没有收到？**

- HTTP Webhook：检查网关响应中 `subscribers` 是否 > 0；若为 0，说明 AlertFly 未订阅到该 channel
- Redis 直连：检查 Redis 服务地址、Channel 名称、AlertFly 订阅模式是否匹配
- Kafka：检查 broker 地址、topic 名称、group_id 是否冲突
- 通用：检查 AlertFly 日志（Web UI → 设置 → 日志级别调为 debug）

**Q2: 消息收到了但没有弹窗？**

- 检查 AlertFly 的过滤配置（`filter.missions`、`filter.senders`、`filter.subtypes`），确认消息字段在接收范围内
- 检查 `notifier.enabled` 是否为 `true`
- 消息仍会存储，可在 Web UI（`http://127.0.0.1:18080`）历史列表中查看

**Q3: Webhook 网关返回 `Token 验证失败`？**

- 检查 `X-Webhook-Token` 请求头是否与网关启动时的 `--token` 参数一致
- 也可通过 `?token=xxx` 查询参数传递

**Q4: Webhook 网关返回 `Redis 发布失败`？**

- 检查网关到 Redis 的网络连通性：`redis-cli -h <host> -p <port> PING`
- 检查 Redis 是否启用 `protected-mode` 导致远程连接被拒（详见 [jenkins-template.md Q6](./jenkins-template.md)）
- 网关会自动重连一次，仍失败请检查 Redis 服务状态

**Q5: 可以同时使用多种通道吗？**

- 可以。AlertFly 支持 Redis 和 Kafka 并行消费；HTTP Webhook 网关最终也是发到 Redis，所以三者可以叠加使用
- 在配置文件中分别启用即可

**Q6: content 字段可以传 JSON 对象吗？**

- `content` 字段类型为字符串。如需传递结构化数据，请将 JSON 序列化为字符串后传入

**Q7: Webhook 网关需要 Redis 客户端库吗？**

- 不需要。网关内置了最小 Redis 客户端（原生 socket 实现 RESP 协议），只需要 Python 3 标准库

---

## 十、参考资料

- [doc/README.md](./README.md) — 文档索引
- [doc/jenkins-template.md](./jenkins-template.md) — Jenkins Post Build Task 集成模板
- [doc/development.md](./development.md) — AlertFly 二次开发指南（Proxy 适配器）
- [doc/build.md](./build.md) — AlertFly 编译与部署指南
- [update-server/README.md](../update-server/README.md) — Python 网关服务部署说明
