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

## 注意事项

- Go 版本要求：1.17+（不使用泛型）
- 编译方式：`go build -mod=vendor ./cmd/alertfly/`
- 新增适配器文件后无需修改 go.mod（纯内部包）
- 如果需要引入新的解析库（如 XML），需要 `go mod tidy` + `go mod vendor` 更新 vendor
