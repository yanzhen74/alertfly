# AlertFly 二次开发指南

本文档面向需要**扩展 AlertFly 消息处理能力**的开发者，说明如何通过 Proxy 适配器层对接自有业务系统的私有消息格式。

> **对接现有 JSON 格式或标准命名规范？** 请阅读 [integration-guide.md](./integration-guide.md)，无需修改代码。

---

## 一、架构概述

```
Redis/Kafka/HTTP Webhook 原始消息
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
                │
                ▼
          Storage 存储 + Notifier 弹窗
```

**核心设计：** Consumer 层先尝试用统一 `model.Message` 结构解析 JSON；解析失败时不丢弃消息，而是通过 Proxy 层按 topic 路由到自定义 Adapter 做二次解析。这种设计让 AlertFly 能对接**任意私有消息格式**，只需编写对应的 Adapter。

---

## 二、统一消息结构

Proxy 层的输出目标结构（`internal/model/message.go`）：

```go
type Message struct {
    ID         int64     `json:"id"`
    Source     string    `json:"source"`      // 消息来源：redis / kafka / webhook
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

字段用途和命名规范详见 [integration-guide.md 第二章](./integration-guide.md#二消息格式)。

---

## 三、Adapter 接口

```go
// internal/proxy/adapter.go
type Adapter interface {
    // Name 返回适配器名称，用于配置路由匹配
    Name() string
    // Parse 将原始字节数据解析为统一 Message
    // rawData 是 Redis/Kafka 收到的原始消息体
    Parse(rawData []byte) (*model.Message, error)
}
```

**路由规则：**

| 方法 | 作用 | 示例 |
|------|------|------|
| `RegisterAdapter(adapter)` | 注册适配器实例 | `px.RegisterAdapter(&JenkinsAdapter{})` |
| `SetTopicAdapter(topic, name)` | 绑定 topic → 适配器 | `px.SetTopicAdapter("jenkins_builds", "jenkins")` |
| `SetDefault(name)` | 设置默认适配器 | `px.SetDefault("json")` |

**路由优先级：** 精确 topic 匹配 > 默认适配器

---

## 四、编写自定义 Adapter

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
px.RegisterAdapter(&proxy.JenkinsAdapter{})     // 新增
px.SetTopicAdapter("jenkins_builds", "jenkins") // 指定 topic 使用 jenkins 适配器
px.SetDefault("json")
```

### 步骤 3：编译验证

```bash
go build -mod=vendor ./cmd/alertfly/
```

---

## 五、更多 Adapter 示例

### 5.1 Prometheus Alertmanager 适配器

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

### 5.2 Zabbix 适配器

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
        Level:      level,
        SubType:    "zabbix",
        Title:      fmt.Sprintf("%s %s @ %s", prefix, raw.Trigger, raw.Host),
        Sender:     raw.Host,
        Content:    raw.Message,
        ReceivedAt: time.Now(),
    }, nil
}
```

---

## 六、Kafka 消费者扩展点

除了 Proxy 适配器，Kafka 消费者本身也有一些可配置的扩展点：

### 6.1 Broker 版本兼容

AlertFly 通过 `kafka.version` 配置项指定 Kafka 协议版本，Sarama 客户端使用该版本号与 broker 协商通信协议。

- 支持 Kafka 0.10.0+
- 版本号格式为 Kafka 官方版本号（如 `"0.10.0.0"`、`"1.0.0"`、`"2.0.0"`、`"3.0.0"`）
- 默认 `"2.0.0"`，低版本 broker 需显式配置

```yaml
kafka:
  version: "1.0.0"  # 与实际 broker 版本匹配
```

配置的版本高于实际 broker 版本时可能导致连接失败或协议不兼容错误。

### 6.2 Topic 正则订阅实现

Kafka 消费者的正则订阅由 `internal/consumer/kafka.go` 中的 `resolveTopics()` 实现：

- 定期扫描（默认 30 秒）broker 上的所有 topic
- 匹配 `regex:` 前缀的包含规则和 `!regex:` 前缀的排除规则
- 扫描失败时自动降级使用上次缓存结果，不影响已有消费
- 精确 topic 和正则 topic 混用时，精确 topic 不受排除规则影响

具体配置示例见 [integration-guide.md 3.2 节](./integration-guide.md#32-kafka-topic-命名点号分隔)。

### 6.3 消息 Topic 字段自动填充

Kafka 消费者在 `ConsumeClaim` 中，如果 topic 名称符合 `alert.{mission}.{sender}.{subtype}.{level}` 格式且 JSON 消息中未提供对应字段，会自动从 topic 名称提取（由 `extractFromTopic()` 实现）。Redis 消费者有对应的 `alert:{...}` 冒号分隔版本。

---

## 七、最佳实践

1. **一个适配器对应一种消息格式**，不要在一个适配器里处理多种格式
2. **Parse 失败时返回 error**，Proxy 会 fallback 到原始消息展示（Title = "Raw Message"）
3. **Title 尽量简短**（弹窗标题），详细信息放 Content
4. **Level 只用三个值**：`info`、`warn`、`error`（话题命名可用 `critical`/`warning`，Adapter 内部需映射）
5. **善用 SubType/Mission/Sender**，方便在 Web UI 中按条件过滤
6. **Topic 可留空**，Proxy 会自动用消息来源的 topic 填充
7. **ReceivedAt 必须设置**，否则 SQLite 存储时会用零值

---

## 八、开发注意事项

- **Go 版本要求**：1.17+（不使用泛型）
- **编译方式**：`go build -mod=vendor ./cmd/alertfly/`
- **新增适配器文件后无需修改 go.mod**（纯内部包）
- **引入新解析库（如 XML）**：需要 `go mod tidy` + `go mod vendor` 更新 vendor 目录
- **测试**：可通过 `scripts/send_alert.sh` 或 Webhook 网关发送测试消息验证 Adapter
- **调试日志**：Web UI → 设置 → 日志级别调为 `debug` 可看到 Proxy 层的解析过程

---

## 九、参考资料

- [integration-guide.md](./integration-guide.md) — 报警对接指南（消息格式、命名规范、Webhook API）
- [build.md](./build.md) — 编译与部署指南
- [jenkins-template.md](./jenkins-template.md) — Jenkins 集成模板
- 源码：
  - `internal/proxy/proxy.go` — Proxy 路由核心
  - `internal/proxy/adapter.go` — Adapter 接口定义 + DefaultJSONAdapter
  - `internal/consumer/redis.go` / `kafka.go` — 消费者实现
  - `internal/model/message.go` — 统一消息结构
