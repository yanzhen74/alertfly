# AlertFly 自更新服务

本目录用于托管 AlertFly 客户端的二进制更新文件，通过 HTTP 服务提供内网自动更新能力。

## 目录结构

```
update-server/
├── start_server.py    # Python HTTP 文件服务器（监听 0.0.0.0:8000）
├── version.json       # 版本信息（客户端首先请求此文件）
├── alertfly           # Linux 二进制
├── alertfly.exe       # Windows 二进制
└── README.md
```

## 工作原理

```
客户端定时 GET version.json
    ↓
比较远程 version 与本地版本
    ↓ (需要更新)
根据 runtime.GOOS 选择 linux_url 或 windows_url
    ↓
下载二进制 → SHA256 校验 → 替换当前文件 → 自动重启
```

客户端通过 `runtime.GOOS` 自动判断平台，Linux 客户端下载 `alertfly`，Windows 客户端下载 `alertfly.exe`，无需服务端区分。

## 快速开始

### 1. 编译并同步更新文件

使用项目根目录的 `build.sh`，编译后会自动同步二进制到本目录并更新 `version.json`：

```bash
# 编译双平台 + 同步 + 打包
./build.sh all

# 仅编译 Linux
./build.sh linux

# 仅编译 Windows
./build.sh windows

# 手动指定版本号
./build.sh all 0.3.0
```

`build.sh` 会自动完成以下操作：
- 编译对应平台的二进制文件到 `build/` 目录
- 复制到本目录（`alertfly` / `alertfly.exe`）
- 计算 SHA256 并更新 `version.json` 中对应字段
- 更新 `version.json` 的 `version` 字段

### 2. 启动更新服务

```bash
cd update-server
python3 start_server.py
```

服务启动后监听 `0.0.0.0:8000`，保持运行即可。每次执行 `build.sh` 后文件会被覆盖更新，**无需重启服务**。

### 3. 客户端配置

在客户端的 `config.yaml` 中配置：

```yaml
updater:
  enabled: true
  check_url: "http://<服务器IP>:8000/version.json"
  interval: "24h"           # 检查间隔，支持 Go duration 格式
```

> **注意**：`check_url` 中的地址必须使用客户端能访问的内网 IP，不要用 `localhost`。

## version.json 字段说明

```json
{
  "version": "0.2.5",
  "linux_url": "http://192.168.1.100:8000/alertfly",
  "linux_sha256": "323bb...",
  "windows_url": "http://192.168.1.100:8000/alertfly.exe",
  "windows_sha256": "4f87c..."
}
```

| 字段 | 说明 |
|------|------|
| `version` | 当前最新版本号（语义化版本，如 `0.2.5`） |
| `linux_url` | Linux 二进制下载地址 |
| `linux_sha256` | Linux 二进制 SHA256 校验值 |
| `windows_url` | Windows 二进制下载地址 |
| `windows_sha256` | Windows 二进制 SHA256 校验值 |

> URL 字段必须使用客户端可访问的内网地址。使用 `build.sh` 时，URL 中的 IP 部分需要手动修改为本服务器的实际内网 IP（默认是 `localhost`）。

## 注意事项

- 服务默认监听 `0.0.0.0:8000`，内网所有机器均可访问
- 服务需保持持续运行，客户端才能正常检查更新
- 更新失败时客户端会保留旧版本继续运行，并通过弹窗告警
- 客户端内置 SHA256 校验，下载文件损坏不会替换
- 连续检查失败时客户端会自动退避（间隔翻倍，上限 24 小时），恢复后自动重试

## 隔离网段离线部署（无互联网环境）

适用于无法连接互联网的服务器环境（如内网、隔离网段）。

**1. 在联网环境准备离线包**

```bash
# 生成 vendor 目录（包含所有依赖）
go mod vendor

# 打包项目（包含 vendor 目录，排除不必要的文件）
tar -czvf alertfly-offline.tar.gz \
  --exclude='.git' \
  --exclude='build/*.tar.gz' \
  --exclude='build/alertfly-*-*' \
  --exclude='*.db' \
  .
```

**2. 传输到隔离网段服务器**

将 `alertfly-offline.tar.gz` 通过安全方式（U 盘、刻录光盘等）传输到目标服务器并解压：

```bash
tar -xzvf alertfly-offline.tar.gz
```

**3. 在隔离网段编译**

```bash
# 使用 vendor 目录编译，无需联网下载依赖
./build.sh linux
```
