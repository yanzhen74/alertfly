# AlertFly 文档索引

AlertFly 是轻量级跨平台桌面告警通知工具，从 Redis / Kafka / HTTP Webhook 接收报警，以桌面弹窗形式提醒用户。

---

## 阅读路径

**根据角色选择入口：**

| 角色 | 目标 | 推荐阅读 |
|------|------|----------|
| **对接方开发者** | 从外部系统发送报警到 AlertFly | [integration-guide.md](./integration-guide.md) |
| **Jenkins 管理员** | 在 Jenkins Job 中集成构建失败报警 | [jenkins-template.md](./jenkins-template.md) |
| **AlertFly 二次开发者** | 扩展消息格式适配器、修改核心逻辑 | [development.md](./development.md) |
| **部署运维** | 编译、部署、更新 AlertFly | [build.md](./build.md) + [../update-server/README.md](../update-server/README.md) |
| **普通用户** | 安装、配置、日常使用 | [../README.md](../README.md) |

---

## 文档清单

### 对接与集成

- **[integration-guide.md](./integration-guide.md)** — 报警对接完整指南
  - 消息格式与字段定义
  - Topic / Channel 命名规范（Redis 冒号 / Kafka 点号）
  - 四种对接方式：HTTP Webhook、Redis PubSub、Redis Stream、Kafka
  - 各方式代码示例（Python / curl / Java / Node.js / redis-cli）
  - 报警级别、过滤机制、常见问题

- **[jenkins-template.md](./jenkins-template.md)** — Jenkins 集成模板
  - Post Build Task 插件配置
  - 构建失败报警发送脚本
  - Redis protected-mode 处理方案
  - Jenkins 常见错误排查

### 开发文档

- **[development.md](./development.md)** — 二次开发指南
  - Proxy 适配器架构
  - 编写自定义 Adapter（Jenkins / Prometheus / Zabbix 示例）
  - Kafka Broker 版本兼容与正则订阅实现
  - 开发注意事项

- **[build.md](./build.md)** — 编译与部署
  - Linux / Windows 交叉编译环境
  - build.sh 使用方法
  - 内网离线部署工具链

### 归档

- **[archive/require.md](./archive/require.md)** — 项目立项前的早期需求稿（历史参考，内容与当前实现不符）

---

## 相关目录

- **[../README.md](../README.md)** — 项目主 README
- **[../update-server/README.md](../update-server/README.md)** — Python 网关服务（自更新 + Webhook）
- **[../config.yaml.example](../config.yaml.example)** — AlertFly 客户端配置示例
- **[../scripts/](../scripts/)** — 手动报警发送脚本（send_alert.sh / send_alert.bat）

---

## 快速开始

**用户端启动 AlertFly：**

```bash
./alertfly --config ./config.yaml
# 打开浏览器访问 http://127.0.0.1:18080
```

**发送第一条测试报警：**

```bash
# 方式 1：通过 HTTP Webhook（推荐，需先部署 Python 网关）
curl -X POST http://<网关IP>:8000/api/webhook \
  -H "Content-Type: application/json" \
  -d '{"title":"测试报警","content":"Hello AlertFly","level":"info"}'

# 方式 2：通过 Redis 直连
./scripts/send_alert.sh "测试报警" "Hello AlertFly" info
```

详见 [integration-guide.md 第七章 7.4 测试验证](./integration-guide.md#74-测试验证)。
