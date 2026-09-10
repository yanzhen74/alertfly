# AlertFly 报警对接接口说明

本文档面向需要向 AlertFly 发送报警消息的外部系统（如监控平台、CI/CD 流水线、业务系统等），说明 AlertFly 的消息接收接口、消息格式及命名规范，供对接开发参考。

---

## 一、概述

AlertFly 是一个轻量级桌面告警通知工具，从 **Redis** 或 **Kafka** 接收报警消息，以桌面弹窗形式提醒用户。它作为消息的**消费端**，外部系统只需按约定格式向 Redis 或 Kafka 发布消息即可。

**对接方式一览：**

| 通道 | 协议 | 模式 | 适用场景 |
|------|------|------|----------|
| Redis PubSub | Redis PUBLISH | 实时发布/订阅 | 即时报警，无需消息持久化 |
| Redis Stream | Redis XADD | 消费者组消费 | 需要消息持久化和可靠投递 |
| Kafka | Producer 写入 | Consumer Group 消费 | 高吞吐、需要消息回溯 |

---

## 二、消息格式

### 2.1 统一消息结构

无论使用哪种通道，消息体均为 **JSON 格式**，字段定义如下：

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
| `title` | string | **是** | 报警标题，弹窗中显示的主标题 |
| `level` | string | 推荐填写 | 报警级别，可选值：`info` / `warn` / `error` |
| `mission` | string | 推荐填写 | 任务/项目名称，用于过滤和分类（如 `deploy`、`infra-monitor`） |
| `sender` | string | 推荐填写 | 发送者标识，表示消息来源系统（如 `prometheus`、`jenkins`、`zabbix`） |
| `subtype` | string | 推荐填写 | 消息子类型，用于内容分类和过滤（如 `cpu_alert`、`disk_alert`、`deploy`） |
| `content` | string | 否 | 报警详情内容，支持自定义 JSON 字符串或纯文本 |
| `source` | string | 否 | 消息来源标识（`redis` 或 `kafka`），通常由 AlertFly 自动填充，无需发送方设置 |
| `topic` | string | 否 | Topic/Channel 名称，通常由 AlertFly 自动填充，无需发送方设置 |

> **注意：** `level`、`mission`、`sender`、`subtype` 虽然标记为非必填，但如果消息 JSON 中未提供这些字段，AlertFly 会尝试从 Topic/Channel 名称中自动解析（见第三章命名规范）。因此建议**尽量在 JSON 中明确填写这些字段**，以确保信息准确。

### 2.2 消息解析优先级

AlertFly 解析消息时遵循以下优先级规则：

1. **JSON 字段优先**：如果 JSON 消息中已包含 `mission`、`sender`、`subtype`、`level` 等字段，以 JSON 中的值为准。
2. **Topic/Channel 名称补充**：JSON 中缺失的字段，AlertFly 会从 Topic 或 Channel 名称中自动提取。
3. **JSON 解析失败降级**：如果消息体不是合法 JSON，AlertFly 会将原始文本作为 `content` 显示，标题标记为 `Raw Message`。

---

## 三、Topic / Channel 命名规范

AlertFly 支持通过 Topic/Channel 名称自动提取消息元数据。命名规范如下：

### 3.1 Redis Channel 命名（冒号分隔）

```
alert:{mission}:{sender}:{subtype}:{level}
```

**示例：**

| Channel 名称 | 解析结果 |
|---|---|
| `alert:deploy:jenkins:build:error` | mission=deploy, sender=jenkins, subtype=build, level=error |
| `alert:infra-monitor:prometheus:cpu_high:warn` | mission=infra-monitor, sender=prometheus, subtype=cpu_high, level=warn |
| `alert:backup:cron:daily:info` | mission=backup, sender=cron, subtype=daily, level=info |

### 3.2 Kafka Topic 命名（点号分隔）

```
alert.{mission}.{sender}.{subtype}.{level}
```

**示例：**

| Topic 名称 | 解析结果 |
|---|---|
| `alert.deploy.jenkins.build.error` | mission=deploy, sender=jenkins, subtype=build, level=error |
| `alert.infra-monitor.prometheus.cpu_high.warn` | mission=infra-monitor, sender=prometheus, subtype=cpu_high, level=warn |

> **重要区分：** Redis 使用**冒号 `:`** 分隔，Kafka 使用**点号 `.`** 分隔。两种格式严格对应，不可混用。

### 3.3 命名规范不是强制的

Topic/Channel 命名规范是**可选的辅助手段**。如果你的系统不使用这种命名格式，只需在 JSON 消息体中明确填写 `mission`、`sender`、`subtype`、`level` 字段即可。AlertFly 会自动适配。

---

## 四、对接方式详解

### 4.1 Redis PubSub 模式

**对接方式：** 外部系统使用 Redis 客户端执行 `PUBLISH` 命令。

**参数：**
- Channel：按命名规范填写，或使用自定义名称（如 `alerts`）
- Message：JSON 格式的消息体

**示例（redis-cli）：**

```bash
redis-cli PUBLISH "alert:deploy:jenkins:build:error" \
  '{"title":"部署失败","level":"error","mission":"deploy","sender":"jenkins","subtype":"build","content":"Pipeline #1234 构建失败，请检查日志"}'
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
- 支持 PSUBSCRIBE 模式匹配订阅（如 `alert:*:jenkins:*:critical`）

### 4.2 Redis Stream 模式

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

### 4.3 Kafka 模式

**对接方式：** 外部系统作为 Kafka Producer，向指定 Topic 写入消息。

**参数：**
- Topic：按命名规范填写，或使用自定义名称
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

**特点：**
- 支持多 Topic 订阅和正则匹配订阅
- 支持 Consumer Group，可多实例负载均衡
- 消息可回溯，offset 自动管理
- 适合高吞吐、多消费者场景

**Kafka 正则订阅说明：**

AlertFly 支持在配置中使用正则表达式匹配 Topic：
- `regex:alert\..*` — 订阅所有以 `alert.` 开头的 Topic
- `!regex:alert\.deploy\..*` — 排除 deploy 任务的报警

外部系统只需按命名规范创建 Topic 并写入消息，AlertFly 会自动发现并消费。

---

## 五、报警级别定义

| 级别 | 含义 | 建议用途 |
|------|------|----------|
| `error` | 严重错误 | 需要立即处理的生产故障、系统宕机等 |
| `warn` | 警告 | 资源使用率超阈值、性能下降等需关注的问题 |
| `info` | 信息 | 部署完成、任务成功等一般性通知 |

---

## 六、过滤机制说明

AlertFly 支持基于以下三个维度进行消息过滤，控制哪些消息弹窗通知：

| 过滤维度 | 对应字段 | 说明 |
|----------|----------|------|
| 任务名 | `mission` | 按项目/任务过滤，如只接收 `deploy` 任务的报警 |
| 发送者 | `sender` | 按来源系统过滤，如只接收 `prometheus` 的报警 |
| 子类型 | `subtype` | 按消息类型过滤，如只接收 `cpu_alert` 类型的报警 |

**重要说明：**
- 过滤仅影响**弹窗通知**，所有消息无论是否匹配过滤条件，都会被**存储到本地数据库**。
- 过滤条件为空表示接收所有（不过滤）。
- 过滤匹配**忽略大小写**。

**对接建议：** 外部系统应尽量在消息中准确填写 `mission`、`sender`、`subtype` 字段，以便用户在 AlertFly 端进行精确过滤。

---

## 七、对接最佳实践

### 7.1 消息发送建议

1. **JSON 字段尽量完整**：虽然只有 `title` 是真正必填的，但提供完整字段可以让 AlertFly 更好地展示和分类。
2. **content 支持自定义格式**：`content` 字段可以是纯文本，也可以是 JSON 字符串，由对接系统自行定义结构。
3. **合理设置 level**：避免所有消息都用同一级别，正确的级别有助于用户区分紧急程度。
4. **mission 和 sender 保持稳定**：同一系统发出的消息应使用固定的 `mission` 和 `sender` 值，便于用户配置过滤规则。

### 7.2 连接建议

1. **Redis 连接**：确保 AlertFly 能访问 Redis 服务地址，注意网络隔离和防火墙配置。
2. **Kafka 连接**：确保 AlertFly 配置中的 `group_id` 与其他消费者不冲突；Kafka broker 版本需与配置一致。
3. **消息编码**：消息体统一使用 **UTF-8** 编码。

### 7.3 测试验证

对接完成后，可使用项目自带的测试脚本验证：

```bash
# Linux/macOS
./scripts/send_alert.sh "测试标题" "测试内容" warn

# Windows
scripts\send_alert.bat "测试标题" "测试内容" warn
```

或使用 Go Mock 工具发送多条测试消息：

```bash
go run scripts/redis_mock_send.go -mode pubsub -channel alert:test:mock:manual:warn
```

---

## 八、完整对接示例

以下是一个完整的对接示例，展示如何从业务系统向 AlertFly 发送报警：

```python
import redis
import json

class AlertFlySender:
    """AlertFly 报警发送器"""

    def __init__(self, redis_addr="localhost:6379", password="", db=0):
        host, port = redis_addr.split(":")
        self.client = redis.Redis(
            host=host, port=int(port), password=password, db=db
        )

    def send(self, title, content, level="warn", mission="", sender="", subtype=""):
        """
        发送报警消息到 AlertFly

        Args:
            title:    报警标题（必填）
            content:  报警详情
            level:    报警级别 (info/warn/error)
            mission:  任务/项目名称
            sender:   发送者标识
            subtype:  消息子类型
        """
        message = {
            "title": title,
            "level": level,
            "mission": mission,
            "sender": sender,
            "subtype": subtype,
            "content": content,
        }

        # 按命名规范构造 channel 名称
        channel = f"alert:{mission}:{sender}:{subtype}:{level}"

        self.client.publish(channel, json.dumps(message, ensure_ascii=False))


# 使用示例
sender = AlertFlySender(redis_addr="192.168.1.100:6379")

sender.send(
    title="订单服务异常",
    content="订单创建接口响应时间超过 5s，当前 P99 = 8.2s",
    level="error",
    mission="order-service",
    sender="apm-monitor",
    subtype="latency_alert",
)
```

---

## 九、常见问题

**Q: 消息发送后 AlertFly 没有收到？**

- 检查 Redis/Kafka 服务地址是否正确
- 检查 AlertFly 配置中的 Channel/Topic 名称是否与发送端一致
- 如果使用模式匹配订阅（如 `alert:*:*:*:*`），确认 Channel 命名符合规范

**Q: 消息收到了但没有弹窗？**

- 检查 AlertFly 的过滤配置（`filter.missions`、`filter.senders`、`filter.subtypes`），确认消息的 `mission`/`sender`/`subtype` 在接收范围内
- 检查 `notifier.enabled` 是否为 `true`

**Q: 可以同时使用 Redis 和 Kafka 吗？**

- 可以。AlertFly 支持 Redis 和 Kafka 并行消费，在配置文件中分别启用即可。

**Q: content 字段可以传 JSON 对象吗？**

- `content` 字段类型为字符串。如果需要传递结构化数据，请将 JSON 序列化为字符串后传入。
