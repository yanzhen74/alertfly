# AlertFly

轻量级跨平台桌面告警通知工具。从 Redis/Kafka 接收告警消息，以不抢夺焦点的系统通知弹窗提醒用户，并提供本地持久化存储和查询过滤。

## 功能特性

- 支持 Redis（PubSub/Stream）和 Kafka（Consumer Group）消息消费
- Proxy 消息适配层，可扩展对接不同系统
- SQLite 本地持久化，支持分页过滤查询
- 跨平台桌面弹窗通知（不抢夺焦点）
- 连接异常弹窗警告
- 软件自更新（HTTP 轮询 + 静默替换 + 自动重启）
- 支持 Ubuntu（X11/Wayland）和 Windows 10/11

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
