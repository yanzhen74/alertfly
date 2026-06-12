# AlertFly

轻量级跨平台桌面告警通知工具。从 Redis/Kafka 接收告警消息，以不抢夺焦点的系统通知弹窗提醒用户，并提供本地持久化存储和查询过滤。

## 功能特性

- 支持 Redis（PubSub/Stream）和 Kafka（Consumer Group）双消息源并行消费
- Proxy 消息适配层，可扩展对接不同系统（Jenkins/Prometheus/Zabbix 等）
- SQLite 本地持久化，支持分页过滤查询
- 跨平台桌面弹窗通知（不抢夺焦点）
  - Linux：notify-send
  - Windows：Shell_NotifyIconW Balloon Tip（Win10/11 自动转 Toast 样式，`-tags win7` 编译 Win7 版本）
- 异步通知 + 限流合并（密集告警自动合并摘要）
- 连接异常弹窗警告（消费者断连自动提醒）
- 软件自更新（HTTP 轮询 + SHA256 校验 + 静默替换）
- 内嵌 Web UI（Gin + Layui）：历史消息查看、配置管理、立即检查更新
- Windows 系统托盘图标 + 右键菜单
- 版本更新等重要事件自动记录到告警历史
- 支持 Ubuntu（X11/Wayland）和 Windows 7/10/11

## 快速开始

### 编译

```bash
# 一键编译 Linux + Windows
./build.sh all

# 指定版本号编译
./build.sh all 0.3.0

# 单独编译 Linux
./build.sh linux

# 单独编译 Windows（需要 mingw-w64 交叉编译工具链）
./build.sh windows
```

编译产物在 `build/` 目录，同时自动更新 `update-server/version.json`。

### 运行

```bash
# 使用配置文件
./alertfly --config ./config.yaml

# 快捷模式
./alertfly --redis-addr localhost:6379
./alertfly --kafka-brokers localhost:9092

# 标准输出模式（管道组合）
./alertfly --config ./config.yaml --stdout
```

## 配置说明

参考 `config.yaml.example` 文件，支持以下配置：

- Redis/Kafka 连接信息
- 消费模式选择（redis/kafka）
- SQLite 存储路径与保留策略
- 自更新开关与轮询地址
- 通知开关

## 手动发送报警

`scripts/` 目录下提供了手动发送报警的脚本（不依赖 Go 环境）：

```bash
# 首次使用，复制配置模板
cp scripts/send_alert.user.example scripts/send_alert.user

# 编辑 Redis 连接等配置
vim scripts/send_alert.user

# 发送报警（Linux）
./scripts/send_alert.sh "CPU过高" "使用率达95%"
./scripts/send_alert.sh "磁盘告警" "空间不足" error

# Windows
scripts\send_alert.bat "服务异常" "API超时"
```

## 二次开发

通过 Proxy 适配器可对接自有业务系统的消息格式，详见 [doc/DEVELOP.md](doc/DEVELOP.md)。

## 自更新配置

AlertFly 支持内网静默自更新，工作流程如下：

### 服务端搭建

发布包中包含 `update-server/` 目录，在任意内网机器上启动更新服务：

```bash
cd update-server/
python3 start_server.py
# 输出: 更新服务已启动: http://0.0.0.0:8000
```

### 版本信息文件

`update-server/version.json` 格式：

```json
{
  "version": "0.2.0",
  "linux_url": "http://192.168.1.100:8000/alertfly",
  "linux_sha256": "Linux二进制的SHA256值",
  "windows_url": "http://192.168.1.100:8000/alertfly.exe",
  "windows_sha256": "Windows二进制的SHA256值"
}
```

- `version`：新版本号（semver 格式）
- `linux_url` / `windows_url`：对应平台的下载地址（客户端自动按平台选取）
- `linux_sha256` / `windows_sha256`：对应二进制的 SHA256 校验值
- 也支持旧格式的 `url` + `sha256` 字段（单平台场景向后兼容）

计算 SHA256：
```bash
# Linux
sha256sum alertfly

# Windows PowerShell
Get-FileHash alertfly.exe -Algorithm SHA256
```

### 客户端配置

在 `config.yaml` 中配置：

```yaml
updater:
  enabled: true
  check_url: "http://192.168.1.100:8000/version.json"
  interval: 1440m    # 检查间隔，默认 24 小时
```

### 更新流程

1. 编译新版本：`./build.sh all`（自动复制二进制到 `update-server/`）
2. 计算 SHA256，更新 `version.json`
3. 客户端下次轮询时自动发现新版本，下载、校验、替换、重启
4. 也可在 Web UI 设置页点击「立即检查更新」手动触发

### 注意事项

- 更新服务和客户端均在内网运行，不访问外网
- 更新服务器需要 Python 3 环境
- Windows 更新时会用临时文件替换后自动重启
- SHA256 校验失败会弹窗警告，不会替换文件

## 系统要求

- Go 1.17+（编译）
- Linux：需安装 `libnotify-bin`（notify-send）
- Windows：Balloon Tip 通知，无额外依赖（Win10+ 自动转 Toast 样式）

## 项目结构

```
alertfly/
├── cmd/alertfly/main.go       -- 程序入口
├── internal/
│   ├── config/                -- 配置加载
│   ├── consumer/              -- Redis/Kafka 消费者
│   ├── proxy/                 -- 消息适配层（可扩展）
│   ├── model/                 -- 数据模型
│   ├── storage/               -- SQLite 存储
│   ├── notifier/              -- 跨平台弹窗通知（异步+限流）
│   ├── tray/                  -- 系统托盘（Windows）
│   ├── updater/               -- 自更新模块
│   └── web/                   -- 内嵌 Web UI
├── scripts/                   -- 工具脚本（mock测试、手动发送等）
├── update-server/             -- 自更新服务端
├── doc/                       -- 文档
│   ├── BUILD.md               -- 编译指南
│   ├── DEVELOP.md             -- 二开说明
│   └── require.md             -- 需求文档
├── build.sh                   -- 一键编译脚本
├── config.yaml.example        -- 配置示例
├── go.mod
└── go.sum
```

## 更新日志

### v0.2.5

- **新增**：Redis PSUBSCRIBE 模式匹配订阅（channel 含通配符时自动切换，从 channel 名称自动提取元数据）
- **新增**：Kafka 正则 topic 订阅（`regex:` 前缀）+ 排除规则（`!regex:` 前缀）
- **新增**：Kafka topic 定期扫描（默认 30 秒，带缓存降级，可配置 `topic_scan_interval`）
- **新增**：Kafka broker 版本可配置化（支持 0.10.0+，默认 2.0.0）
- **说明**：Redis/Kafka 话题命名规范（四段式：任务.发送者.报警类别.报警级别）
- **优化**：版本检查失败抑制 + 指数退避（连续相同失败只记第一条，间隔翻倍上限 24h）

### v0.2.4

- **新增**：设置页面「接收过滤」功能（可按 Mission/Sender/SubType 过滤弹窗通知）
- **新增**：消息列表增加「子类型」列
- **说明**：不匹配过滤条件的消息仍会存储，仅不弹窗；留空表示全部接收

### v0.2.3

- **Bug 修复**：修复 Windows 通知不弹出的严重 bug（vendor/systray `notifyIconData` 结构体 union 字段对齐错误，导致所有后续字段偏移+4字节）
- **改进**：Windows 通知现支持全版本（Win7 Balloon Tip / Win10+11 自动转 Toast 样式）
- **改进**：分版本 build tag，默认编译 Win10/11，`-tags win7` 编译 Win7 版本
- **改进**：移除 go-toast/toast 依赖（消除 PowerShell 3-6秒启动延迟）
- **改进**：异步通知限流间隔优化为 200ms

## License

MIT
