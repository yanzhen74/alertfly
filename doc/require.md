好的，针对 **AlertFly** 这个项目，为你整理了一份清晰的需求文档 + 技术方案建议。

---

## 一、项目名称

**AlertFly**

> GitHub 仓库名建议：`alertfly`

---

## 二、需求概述

### 1. 项目定位
一个轻量级、跨系统的桌面通知工具，从 Redis / Kafka 接收告警消息，以**不抢夺焦点**的弹窗形式提醒用户，并提供历史消息查看与查询过滤界面。

### 2. 核心功能需求

| 功能 | 说明 |
|------|------|
| 接收告警 | 支持从 Redis（Pub/Sub 或 Stream）和 Kafka（Consumer Group）拉取消息 |
| 弹窗提醒 | 桌面原生风格弹窗，**不夺取当前窗口焦点** |
| 历史记录 | 本地持久化存储收到的消息，支持分页查看 |
| 订阅设置 | 用户根据过滤字段实现进行订阅设置、网络设置等 |
| 过滤查询 | 按时间、关键字、来源系统、级别（如 error/warning/info）等过滤 |
| 界面 | 用户可主动打开的独立 GUI 窗口，用于查看历史和过滤 |
| 跨平台 | 支持 Ubuntu（Wayland/X11）和 Windows 10/11 |
| 部署方式 | 桌面版直接点击运行；命令行版通过命令行运行.无需额外安装复杂依赖 |

### 3. 非功能需求

- **轻量**：内存占用低，不随系统启动（除非用户自配）
- **可靠**：消息至少收到一次（at-least-once），支持断网恢复后继续消费
- **不交互**：纯接收 + 本地展示，不向源系统回传任何确认（除 Kafka offset 提交外）

### 4. 明确不做的

- ❌ 不实现双向通信、不回复消息
- ❌ 不做复杂的告警规则引擎
- ❌ 不集成钉钉、邮件等外部发送能力（你说的邮件通知仅作为备用 demo 方式，不内置）

---

## 三、技术方案建议

### 整体架构

```text
[ Redis/Kafka ] 
     ↓
AlertFly Core (consumer + storage)
     ↓
├── 弹窗模块（无焦点弹窗）
└── GUI 历史查看窗口（Tauri / Flutter / Fyne）
```

### 推荐技术栈

| 模块 | 选择 | 理由 |
|------|------|------|
| 语言 | **Go** | 跨平台、单二进制、并发模型优秀、Redis/Kafka 客户端成熟 |
| 弹窗库 | **go-toast** (Win) + **notify-send** (Linux) | 原生、不抢焦点 |
| GUI 历史界面 | **Fyne** 或 **Wails** | 轻量、Go 原生或接近原生、打包简单 |
| 配置 | 环境变量 + 配置文件（YAML/TOML） | 简单清晰 |
| 存储 | **SQLite** | 零配置、嵌入式、支持 SQL 查询过滤 |
| Redis 客户端 | `go-redis/redis` | 稳定、支持 PubSub/Stream |
| Kafka 客户端 | `IBM/sarama` | Go 社区最成熟的 Kafka 库 |

### 为什么不选 Rust？

Rust 完全可以实现，但：
- 桌面 GUI 库生态相对复杂（`tauri` 需系统 webview，`iced` 尚不成熟）
- 跨平台弹窗不抢焦点的细节处理更麻烦
- Go 在运维工具领域更“原生”，未来他人维护成本更低

> 如果你希望保持纯 Rust 技术栈（为了学习或统一），也可以，届时需要调整 GUI 为 `tauri` + 前端。

---

## 四、模块详细设计

### 1. 消费者模块

- 同时支持 Redis 和 Kafka，通过配置选择
- 支持连接参数、topic/channel、group-id 等配置
- 启动后持续拉取，消息到达后 → 交给弹窗模块 + 存储模块

### 2. 存储模块

- 表结构建议：

```sql
messages (
  id         INTEGER PRIMARY KEY,
  source     TEXT,   -- redis / kafka
  topic      TEXT,
  level      TEXT,   -- info/warn/error
  subtype    TEXT,   -- 对应消息的SubType,过滤字段
  title      TEXT,   
  mission    TEXT,   -- 任务名称，过滤字段
  sender     TEXT,   -- 发送者,过滤字段
  content    TEXT,   -- 内容是随SubType而变的自定义json格式
  received_at DATETIME
)
```

- 历史记录保留策略：可配置（如 7 天或 10000 条）

### 3. 弹窗模块（不抢焦点）

| 平台 | 实现方式 |
|------|----------|
| Windows | `go-toast` 或直接调用 `ToastNotificationManager` |
| Ubuntu | 调用 `notify-send`（需确保 `libnotify-bin` 已安装） |

**关键点**：弹窗必须使用系统通知机制，而不是 `MessageBox` 或自定义模态窗口，否则会抢焦点。

### 4. GUI 历史查看窗口

推荐使用 **Fyne**（纯 Go）：

- 无需额外依赖，单文件
- 支持表格、输入框、时间选择器
- 可以在弹窗出现时自动刷新列表

界面布局（需要根据过滤字段补充完善）：

```
[ 过滤器区域 ]  关键字: ____  级别: [▼]  时间: [起始] ~ [结束]  [查询]
-------------------------------------------------------------------
| 时间            | 来源  | 级别   | 内容摘要               |
| 2025-... 10:23  | redis | error  | disk full /dev/sda1   |
| ...             | ...   | ...    | ...                   |
-------------------------------------------------------------------
[ 上一页 ] [ 下一页 ]
```

### 5. 命令行使用方式

```bash
# 使用配置文件
alertfly --config ./config.yaml

# 或直接指定连接方式（单次运行，适用于测试邮件通知替代）
alertfly --redis-addr localhost:6379 --channel alarms
```

#### 备用“邮件替代”模式

你提到的“通过邮件通知即可，不用额外开发”可以解释为：
> AlertFly **不需要**内置邮件发送功能，但如果用户想用 mail 当备用通知方式，可以自己在命令行用 `alertfly + mail` 管道组合。

例如用户可自行编写脚本：

```bash
alertfly --stdout | while read msg; do echo "$msg" | mail -s "Alert" user@example.com; done
```

这种方式符合“不额外开发、只靠命令行”的要求。

---

## 五、项目结构建议

```text
alertfly/
├── cmd/
│   └── alertfly/
│       └── main.go
├── internal/
│   ├── consumer/
│   │   ├── redis.go
│   │   └── kafka.go
│   ├── notifier/
│   │   ├── notifier.go
│   │   ├── windows.go
│   │   └── linux.go
│   ├── storage/
│   │   └── sqlite.go
│   └── gui/
│       └── window.go   (Fyne)
├── config.yaml.example
├── go.mod
├── go.sum
├── README.md
└── LICENSE
```

---

## 六、典型使用场景示例

**场景**：运维人员不希望被频繁弹窗打断工作，但必须实时看到来自 Kafka 的告警。

运行：

```bash
alertfly --kafka-brokers localhost:9092 --topic alerts
```

- 工作时弹窗不会抢走 VSCode / 终端焦点
- 需要回顾时，点击托盘图标或快捷键打开历史窗口
- 下班前按级别过滤查看当天所有 `error` 告警

---

## 七、下一步建议

如果你希望我帮你：
1. **生成完整代码框架**（Go + SQLite + Fyne GUI + 跨平台弹窗）
2. **只实现 CLI 版本 + 弹窗**（GUI 后续再加）
3. **调整为 Rust 技术栈版本**
4. **画出更具体的系统交互时序图**

可以告诉我你的偏好，我可以基于这份需求直接落地实现。