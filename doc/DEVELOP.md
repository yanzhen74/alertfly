# AlertFly 二次开发指南

本文档说明如何通过 Proxy 适配器层对接自有业务系统的消息格式。

## 架构概述

```
Redis/Kafka 原始消息
        │
        ▼
  Consumer 层（尝试 JSON 解析为 model.Message）
        │
        ├── 解析成功 → 直接使用
        │
        └── 解析失败（Title == "Raw Message"）
                │
                ▼
          Proxy 层（按 topic 路由到自定义 Adapter 二次解析）
                │
                ▼
          model.Message（统一格式）
```

## 统一消息结构

```go
type Message struct {
    ID         int64     `json:"id"`
    Source     string    `json:"source"`      // 消息来源：redis / kafka
    Topic      string    `json:"topic"`       // topic 或 channel 名称
    Level      string    `json:"level"`       // 告警级别：info / warn / error
    SubType    string    `json:"subtype"`     // 消息子类型（过滤字段）
    Title      string    `json:"title"`       // 消息标题（弹窗显示）
    Mission    string    `json:"mission"`     // 任务名称（过滤字段）
    Sender     string    `json:"sender"`      // 发送者（过滤字段）
    Content    string    `json:"content"`     // 消息内容（弹窗正文）
    ReceivedAt time.Time `json:"received_at"` // 接收时间
}
```

## 编写自定义 Adapter

### 步骤 1：创建适配器文件

在 `internal/proxy/` 目录下新建文件，例如 `adapter_jenkins.go`：

```go
package proxy

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/oliverxu/alertfly/internal/model"
)

// JenkinsAdapter 解析 Jenkins 构建通知消息
type JenkinsAdapter struct{}

func (a *JenkinsAdapter) Name() string {
    return "jenkins"
}

func (a *JenkinsAdapter) Parse(rawData []byte) (*model.Message, error) {
    // 定义你的业务系统原始消息格式
    var raw struct {
        JobName    string `json:"job_name"`
        BuildNum   int    `json:"build_number"`
        Status     string `json:"status"`     // SUCCESS / FAILURE / UNSTABLE
        Duration   int    `json:"duration_ms"`
        Trigger    string `json:"triggered_by"`
        URL        string `json:"build_url"`
    }

    if err := json.Unmarshal(rawData, &raw); err != nil {
        return nil, fmt.Errorf("jenkins parse failed: %w", err)
    }

    // 映射告警级别
    level := "info"
    switch raw.Status {
    case "FAILURE":
        level = "error"
    case "UNSTABLE":
        level = "warn"
    }

    // 转换为统一 Message
    return &model.Message{
        Source:     "redis",
        Level:      level,
        SubType:    "ci",
        Title:      fmt.Sprintf("[Jenkins] %s #%d %s", raw.JobName, raw.BuildNum, raw.Status),
        Mission:    raw.JobName,
        Sender:     raw.Trigger,
        Content:    fmt.Sprintf("构建耗时: %dms\n详情: %s", raw.Duration, raw.URL),
        ReceivedAt: time.Now(),
    }, nil
}
```

### 步骤 2：注册适配器

编辑 `cmd/alertfly/main.go`，在 Proxy 初始化处注册：

```go
// --- 初始化 Proxy ---
px := proxy.NewProxy()
px.RegisterAdapter(&proxy.DefaultJSONAdapter{})
px.RegisterAdapter(&proxy.JenkinsAdapter{})    // 新增
px.SetTopicAdapter("jenkins_builds", "jenkins") // 指定 topic 使用 jenkins 适配器
px.SetDefault("json")
```

### 步骤 3：编译验证

```bash
go build -mod=vendor ./cmd/alertfly/
```

## Adapter 接口说明

```go
type Adapter interface {
    // Name 返回适配器名称，用于配置路由匹配
    Name() string
    // Parse 将原始字节数据解析为统一 Message
    // rawData 是 Redis/Kafka 收到的原始消息体
    Parse(rawData []byte) (*model.Message, error)
}
```

## 路由规则

| 方法 | 作用 | 示例 |
|------|------|------|
| `RegisterAdapter(adapter)` | 注册适配器实例 | `px.RegisterAdapter(&JenkinsAdapter{})` |
| `SetTopicAdapter(topic, name)` | 绑定 topic → 适配器 | `px.SetTopicAdapter("jenkins_builds", "jenkins")` |
| `SetDefault(name)` | 设置默认适配器 | `px.SetDefault("json")` |

**路由优先级**：精确 topic 匹配 > 默认适配器

## 更多示例

### Prometheus Alertmanager 适配器

```go
type AlertmanagerAdapter struct{}

func (a *AlertmanagerAdapter) Name() string { return "alertmanager" }

func (a *AlertmanagerAdapter) Parse(rawData []byte) (*model.Message, error) {
    var raw struct {
        Status string `json:"status"`  // firing / resolved
        Labels struct {
            AlertName string `json:"alertname"`
            Severity  string `json:"severity"`
            Instance  string `json:"instance"`
        } `json:"labels"`
        Annotations struct {
            Summary     string `json:"summary"`
            Description string `json:"description"`
        } `json:"annotations"`
    }

    if err := json.Unmarshal(rawData, &raw); err != nil {
        return nil, err
    }

    level := "info"
    switch raw.Labels.Severity {
    case "critical":
        level = "error"
    case "warning":
        level = "warn"
    }

    title := fmt.Sprintf("[%s] %s (%s)", raw.Status, raw.Labels.AlertName, raw.Labels.Instance)

    return &model.Message{
        Level:      level,
        SubType:    "prometheus",
        Title:      title,
        Sender:     raw.Labels.Instance,
        Content:    raw.Annotations.Description,
        ReceivedAt: time.Now(),
    }, nil
}
```

### Zabbix 适配器

```go
type ZabbixAdapter struct{}

func (a *ZabbixAdapter) Name() string { return "zabbix" }

func (a *ZabbixAdapter) Parse(rawData []byte) (*model.Message, error) {
    var raw struct {
        EventID  string `json:"event_id"`
        Host     string `json:"host"`
        Trigger  string `json:"trigger_name"`
        Severity string `json:"severity"`  // Disaster/High/Average/Warning/Information
        Status   string `json:"status"`    // PROBLEM / RESOLVED
        Message  string `json:"message"`
    }

    if err := json.Unmarshal(rawData, &raw); err != nil {
        return nil, err
    }

    level := "info"
    switch raw.Severity {
    case "Disaster", "High":
        level = "error"
    case "Average", "Warning":
        level = "warn"
    }

    prefix := "🔴 PROBLEM"
    if raw.Status == "RESOLVED" {
        prefix = "✅ RESOLVED"
        level = "info"
    }

    return &model.Message{
        Level:   level,
        SubType: "zabbix",
        Title:   fmt.Sprintf("%s %s @ %s", prefix, raw.Trigger, raw.Host),
        Sender:  raw.Host,
        Content: raw.Message,
        ReceivedAt: time.Now(),
    }, nil
}
```

## 最佳实践

1. **一个适配器对应一种消息格式**，不要在一个适配器里处理多种格式
2. **Parse 失败时返回 error**，Proxy 会 fallback 到原始消息展示
3. **Title 尽量简短**（弹窗标题），详细信息放 Content
4. **Level 只用三个值**：`info`、`warn`、`error`
5. **善用 SubType/Mission/Sender**，方便在 Web UI 中按条件过滤
6. **Topic 可留空**，Proxy 会自动用消息来源的 topic 填充

## Redis 话题命名规范与订阅过滤

Redis PubSub 原生支持模式匹配订阅（`PSUBSCRIBE`），通过合理的 channel 命名规范，可以灵活地按任务、发送者、类别、级别进行过滤订阅。

### 话题格式

```
alert:{任务}:{发送者}:{报警类别}:{报警级别}
```

各段定义：

| 段 | 说明 | 示例 |
|---|---|---|
| 任务 (mission) | 业务任务名称 | deploy、backup、monitor |
| 发送者 (sender) | 系统/服务来源 | crm、erp、gateway |
| 报警类别 (subtype) | 具体报警分类 | cpu-high、disk-full、oom、timeout |
| 报警级别 (level) | 严重程度 | critical、error、warning |

### 完整示例

```
alert:deploy:crm:timeout:error
alert:backup:erp:disk-full:critical
alert:monitor:gateway:cpu-high:warning
```

### 订阅灵活性示例（利用 Redis PSUBSCRIBE 模式匹配）

| 订阅模式 | 含义 |
|----------|------|
| `alert:taska:*:*:*` | 接收 taska 任务的所有报警 |
| `alert:*:crm:*:critical` | 接收 CRM 系统的所有严重报警 |
| `alert:taska:crm:cpu-high:*` | 接收 taska+CRM+CPU过高的所有级别 |
| `alert:*:*:*:critical` | 接收所有系统的严重报警 |
| `alert:taska:crm:*:critical` | 接收 taska+CRM 的所有严重报警 |
| `alert:*:*:cpu-high:*` | 接收所有系统的 CPU 过高报警 |
| `alert:deploy:*:*:*` | 接收所有部署任务的报警 |

### 发送端示例（Python）

```python
import redis
import json

r = redis.Redis(host='localhost', port=6379)

# 按规范发送
channel = "alert:deploy:crm:timeout:error"
message = json.dumps({
    "title": "CRM部署超时",
    "content": "部署任务超过30分钟未完成",
    "mission": "deploy",
    "sender": "crm",
    "subtype": "timeout",
    "level": "error"
})
r.publish(channel, message)
```

### 发送端示例（redis-cli）

```bash
redis-cli PUBLISH "alert:monitor:gateway:cpu-high:warning" \
  '{"title":"网关CPU过高","content":"CPU使用率95%","mission":"monitor","sender":"gateway","subtype":"cpu-high","level":"warning"}'
```

### AlertFly 配置说明

在 `config.yaml` 中配置订阅模式：

```yaml
redis:
  enabled: true
  addr: "localhost:6379"
  mode: "pubsub"
  channel: "alert:*:*:*:*"  # 接收所有报警（模式匹配）
  # channel: "alert:*:crm:*:critical"  # 只接收CRM严重报警
```

### 注意事项

- 使用模式匹配订阅时，AlertFly 的 Redis 消费者会自动使用 `PSubscribe`（模式订阅）而非 `Subscribe`（精确订阅）
- **AlertFly 已支持 `PSUBSCRIBE` 模式匹配**：当 channel 配置中包含通配符（`*`、`?`、`[`）时，自动切换为模式匹配订阅；不含通配符时仍使用精确订阅
- 模式匹配订阅时，消息的 Topic 字段使用实际匹配到的 channel 名称（如 `alert:deploy:crm:timeout:error`），而非配置的模式
- 如果 channel 名称符合 `alert:{mission}:{sender}:{subtype}:{level}` 格式，且 JSON 消息中未提供对应字段，AlertFly 会自动从 channel 名称中提取这些元数据
- 报警级别只定义三个：`critical`（严重）、`error`（错误）、`warning`（警告）
- 各段中不要使用冒号(`:`)，避免与分隔符冲突

## 消息发送端示例

发送端只需向 Redis channel 或 stream 推送 JSON 即可：

```python
# Python 示例 - 发送到 Redis PubSub
import redis, json

r = redis.Redis()
msg = {
    "job_name": "deploy-prod",
    "build_number": 142,
    "status": "FAILURE",
    "duration_ms": 32000,
    "triggered_by": "oliver",
    "build_url": "http://jenkins.local/job/deploy-prod/142"
}
r.publish("jenkins_builds", json.dumps(msg))
```

```bash
# redis-cli 手动测试
redis-cli PUBLISH jenkins_builds '{"job_name":"test","build_number":1,"status":"SUCCESS","duration_ms":1000,"triggered_by":"ci","build_url":"http://localhost"}'
```

## Kafka Broker 版本兼容性

AlertFly 通过 `kafka.version` 配置项指定 Kafka 协议版本，Sarama 客户端会使用该版本号与 broker 协商通信协议。

- 支持 Kafka 0.10.0+
- 版本号格式为 Kafka 官方版本号（如 `"0.10.0.0"`、`"1.0.0"`、`"2.0.0"`、`"3.0.0"`）
- 默认 `"2.0.0"`，低版本 broker 需显式配置

配置示例：

```yaml
kafka:
  version: "1.0.0"  # 与实际 broker 版本匹配
```

如果配置的版本高于实际 broker 版本，可能导致连接失败或协议不兼容错误。遇到此类问题时，请将 `version` 设置为与 broker 一致或低于 broker 的版本号。

## Kafka Topic 正则订阅

Kafka 消费者支持在 topics 列表中使用 `regex:` 前缀标识正则模式，实现动态 topic 订阅。

### 配置格式

```yaml
kafka:
  enabled: true
  brokers:
    - "localhost:9092"
  topics:
    - "alert-events"                        # 精确订阅
    - "regex:alert-.*"                      # 正则：所有 alert- 前缀的 topic
    - "regex:^(deploy|monitor)-alerts$"     # 正则：指定前缀的 alerts topic
  group_id: "alertfly_group"
```

### 匹配效果示例

| 配置 | 匹配效果 |
|------|-----------|
| `alert-events` | 精确匹配 alert-events |
| `regex:alert-.*` | 匹配所有 alert- 开头的 topic |
| `regex:^alert-(deploy\|monitor)-.*` | 匹配 alert-deploy-* 和 alert-monitor-* |
| `regex:.*-critical$` | 匹配所有以 -critical 结尾的 topic |
| `!regex:alert\.deploy\..*` | 排除所有 deploy 任务报警 | 配合 `regex:alert\..*` 使用 |
| `!regex:alert\..*\..*\..*\.warning` | 排除所有 warning 级别 | 只接收 critical 和 error |

### 说明

- 正则匹配定期执行（默认 30 秒扫描一次，可通过 `topic_scan_interval` 配置），能自动发现新建的 topic
- 精确 topic 和正则 topic 可以混用
- 正则语法为 Go 标准库 regexp（RE2 语法）
- 正则匹配到的 topic 会自动去重
- 旧的单 topic 配置（`topic: "alerts"`）仍然兼容，会自动迁移到 topics 列表
- `!regex:` 为排除规则，匹配到的 topic 会被移除；排除规则在包含规则之后执行；可以同时有多条排除规则；精确 topic 不受排除规则影响（精确指定的始终保留）

### 注意事项

- 正则匹配依赖从 broker 获取所有 topic 列表（`sarama.Client.Topics()`），需要 broker 支持
- 如果正则匹配结果为空（broker 上暂无匹配 topic），消费者会按扫描间隔重试
- 扫描失败时自动降级使用上次缓存的结果，不影响已有消费
- 消息的 Topic 字段使用实际消费到的 topic 名称（而非配置的模式），与 Redis PSUBSCRIBE 行为一致

## Kafka 话题命名规范

Kafka 的 alert 话题格式与 Redis 一样（四段式），但分隔符从冒号 `:` 改为点号 `.`。

### 话题格式

```
alert.{任务}.{发送者}.{报警类别}.{报警级别}
```

与 Redis 话题规范对应关系：

| 段 | Redis 格式 | Kafka 格式 |
|---|---|---|
| 分隔符 | `:` | `.` |
| 完整格式 | `alert:{mission}:{sender}:{subtype}:{level}` | `alert.{mission}.{sender}.{subtype}.{level}` |

报警级别（同 Redis）：critical / error / warning

### 完整示例

```
alert.deploy.crm.timeout.error
alert.backup.erp.disk-full.critical
alert.monitor.gateway.cpu-high.warning
```

### 正则订阅示例

| 配置 | 匹配效果 | 举例匹配到的 topic |
|------|----------|--------------------|
| `regex:alert\..*` | 所有 alert 话题 | alert.deploy.crm.timeout.error, alert.monitor.gateway.cpu-high.warning |
| `regex:alert\.deploy\..*` | 所有部署任务报警 | alert.deploy.crm.timeout.error, alert.deploy.erp.failed.critical |
| `regex:alert\..*\.crm\..*\.critical` | CRM 系统所有严重报警 | alert.deploy.crm.timeout.critical, alert.monitor.crm.oom.critical |
| `regex:alert\..*\..*\.cpu-high\..*` | 所有 CPU 过高报警 | alert.monitor.gateway.cpu-high.warning, alert.monitor.crm.cpu-high.error |
| `regex:alert\.deploy\.crm\..*\..*` | deploy+CRM 的所有报警 | alert.deploy.crm.timeout.error, alert.deploy.crm.failed.critical |
| `!regex:alert\.deploy\..*` | 排除所有 deploy 任务 | 配合 `regex:alert\..*` 使用 |
| `!regex:alert\..*\..*\..*\.warning` | 排除所有 warning 级别 | 只接收 critical 和 error |

> 注意：正则中 `.` 需要转义为 `\.`（否则匹配任意字符）

### AlertFly 自动填充行为

如果 topic 名称符合 `alert.{mission}.{sender}.{subtype}.{level}` 格式，且 JSON 消息中未提供对应字段，AlertFly 会自动从 topic 名称中提取这些元数据（JSON 字段优先）。

### 发送端示例（kafka-console-producer）

```bash
echo '{"title":"CRM部署超时","content":"部署超过30分钟","mission":"deploy","sender":"crm","subtype":"timeout","level":"error"}' | \
  kafka-console-producer --broker-list localhost:9092 --topic alert.deploy.crm.timeout.error
```

## 注意事项

- Go 版本要求：1.17+（不使用泛型）
- 编译方式：`go build -mod=vendor ./cmd/alertfly/`
- 新增适配器文件后无需修改 go.mod（纯内部包）
- 如果需要引入新的解析库（如 XML），需要 `go mod tidy` + `go mod vendor` 更新 vendor
