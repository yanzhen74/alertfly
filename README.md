# AlertFly

轻量级跨平台桌面告警通知工具。从 Redis/Kafka 接收告警消息，以不抢夺焦点的系统通知弹窗提醒用户，并提供本地持久化存储和查询过滤。

## 功能特性

- 支持 Redis（PubSub/Stream）和 Kafka（Consumer Group）消息消费
- Proxy 消息适配层，可扩展对接不同系统
- SQLite 本地持久化，支持分页过滤查询
- 跨平台桌面弹窗通知（不抢夺焦点）
- 连接异常弹窗警告
- 软件自更新（HTTP 轮询 + SHA256 校验 + 静默替换 + 自动重启）
- 内嵌 Web UI（Gin + Layui）：历史消息查看、配置管理
- Windows 系统托盘图标 + 气泡通知（兼容 Win7）
- 支持 Ubuntu（X11/Wayland）和 Windows 7/10/11

## 快速开始

### 编译

```bash
# Linux
go build -mod=vendor -o alertfly ./cmd/alertfly/

# Windows（交叉编译）
GOOS=windows GOARCH=amd64 go build -mod=vendor -o alertfly.exe ./cmd/alertfly/
```

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
- Windows：原生 Toast 通知，无额外依赖

## 项目结构

```
alertfly/
├── cmd/alertfly/main.go       -- 程序入口
├── internal/
│   ├── config/                -- 配置加载
│   ├── consumer/              -- Redis/Kafka 消费者
│   ├── proxy/                 -- 消息适配层
│   ├── model/                 -- 数据模型
│   ├── storage/               -- SQLite 存储
│   ├── notifier/              -- 跨平台弹窗通知
│   └── updater/               -- 自更新模块
├── config.yaml.example        -- 配置示例
├── go.mod
└── go.sum
```

## License

MIT
