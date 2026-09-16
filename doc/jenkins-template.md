# Jenkins 构建失败报警接入（Post Build Task）

本文档说明如何在 Jenkins Freestyle Job 中通过 **Post Build Task** 插件，实现**构建失败时**自动向 AlertFly 发送报警。使用 AlertFly 默认 JSON 格式，无需修改任何 Go 代码。

---

## 一、准备工作（一次性）

### 1.1 安装 Post Build Task 插件

Jenkins → **Manage Jenkins** → **Plugins** → **Available plugins** → 搜索 `Post Build Task` → 安装。

### 1.2 Jenkins 节点安装 redis-cli

```bash
# Ubuntu / Debian
sudo apt-get install -y redis-tools

# CentOS / RHEL
sudo yum install -y redis
```

验证：`redis-cli -h <REDIS_HOST> -p 6379 PING` 应返回 `PONG`。

### 1.3 创建发送脚本

在 Jenkins 节点上创建 `/var/lib/jenkins/alertfly-send.sh`：

```bash
#!/bin/sh
# alertfly-send.sh —— 向 AlertFly 发送 Jenkins 构建通知
# 用法: alertfly-send.sh <level> [title] [content] [log_lines]
#   log_lines: 附加的构建日志行数，默认 5，也可用环境变量 ALERTFLY_LOG_LINES 指定
#
# 使用 POSIX sh 兼容语法，避免 Jenkins 用 dash 执行时报错
set -eu

LEVEL="${1:-error}"
TITLE="${2:-${JOB_NAME} #${BUILD_NUMBER}}"
CONTENT="${3:-URL: ${BUILD_URL}}"
LOG_LINES="${4:-${ALERTFLY_LOG_LINES:-5}}"

REDIS_HOST="${ALERTFLY_REDIS_HOST:-192.168.1.100}"

# 取日志最后 N 行，并截掉 "Performing Post build task..." 标记行及其之后的
# post-build 自身输出，保证只展示真实构建步骤的日志
# 依赖入参：$LOG_SRC（日志文件路径）、$N（行数）
tail_n_before_marker() {
    # 标记行之前（不含标记行）的所有行；正则用 [Pp] 做大小写兼容，gawk/mawk 均可运行
    # tr 清洗控制字符（保留 \t 和 ESC）：ANSI 颜色码的 ESC 由 esc() 转义为 \u001b，
    # 其余裸控制字符（如 \x07）会导致 JSON 非法，必须删除
    LINES=$(awk '/[Pp]ost build task/{exit} {print}' "$LOG_SRC" | tr -d '\000-\010\013\014\016-\032\034-\037\177')
    if [ -n "$LINES" ]; then
        printf '%s\n' "$LINES" | tail -n "$N"
    else
        # 日志中找不到标记行（或标记行在首行）时，降级取文件末尾 N 行
        tail -n "$N" "$LOG_SRC" | tr -d '\000-\010\013\014\016-\032\034-\037\177'
    fi
}

# 附加构建日志（标记行之前的最后 N 行）到 content
# 优先读本地 log 文件，其次通过 HTTP 拉取 consoleText
LOG_TAIL=""
if [ -n "${JENKINS_HOME:-}" ] && [ -n "${JOB_NAME:-}" ] && [ -n "${BUILD_NUMBER:-}" ]; then
    # JOB_NAME 含 '/'（文件夹 Job）时，日志路径中 '/' 要替换为 '/jobs/'
    JOB_PATH=$(printf '%s' "$JOB_NAME" | sed 's|/|/jobs/|g')
    LOG_FILE="${JENKINS_HOME}/jobs/${JOB_PATH}/builds/${BUILD_NUMBER}/log"
    if [ -r "$LOG_FILE" ]; then
        LOG_SRC="$LOG_FILE"
        N="$LOG_LINES"
        LOG_TAIL=$(tail_n_before_marker)
    fi
fi
if [ -z "$LOG_TAIL" ] && [ -n "${BUILD_URL:-}" ] && command -v curl >/dev/null 2>&1; then
    CONSOLE_FILE="${TMPDIR:-/tmp}/alertfly-console-$$"
    if curl -s --max-time 5 "${BUILD_URL}consoleText" > "$CONSOLE_FILE" && [ -s "$CONSOLE_FILE" ]; then
        LOG_SRC="$CONSOLE_FILE"
        N="$LOG_LINES"
        LOG_TAIL=$(tail_n_before_marker)
    fi
    rm -f "$CONSOLE_FILE"
fi
if [ -n "$LOG_TAIL" ]; then
    CONTENT="${CONTENT}
--- 构建日志(最后${LOG_LINES}行) ---
${LOG_TAIL}"
fi
REDIS_PORT="${ALERTFLY_REDIS_PORT:-6379}"
MISSION="${ALERTFLY_MISSION:-build}"
SUBTYPE="${ALERTFLY_SUBTYPE:-build}"

# JSON 转义：处理 \ " 换行 回车 Tab（用 awk 实现，POSIX 兼容）
# 日志中的 { } 等字符在 JSON 字符串内无需转义，原样保留不会破坏结构；
# 日志的 ANSI 控制字符已在 tail_n_before_marker 中清洗
esc() {
    printf '%s' "$1" | awk '
        BEGIN { ORS=""; esc = sprintf("%c", 27) }
        {
            gsub(/\\/, "\\\\")
            gsub(/"/, "\\\"")
            gsub(/\t/, "\\t")
            gsub(/\r/, "\\r")
            gsub(esc, "\\u001b")
            if (NR > 1) print "\\n"
            print
        }
    '
}

ESC_TITLE=$(esc "$TITLE")
ESC_MISSION=$(esc "$MISSION")
ESC_SUBTYPE=$(esc "$SUBTYPE")
ESC_CONTENT=$(esc "$CONTENT")

CHANNEL="alert:${MISSION}:jenkins:${SUBTYPE}:${LEVEL}"
PAYLOAD=$(printf '{"title":"%s","level":"%s","mission":"%s","sender":"jenkins","subtype":"%s","content":"%s"}' \
    "$ESC_TITLE" "$LEVEL" "$ESC_MISSION" "$ESC_SUBTYPE" "$ESC_CONTENT")

redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" PUBLISH "$CHANNEL" "$PAYLOAD"
```

赋予执行权限：

```bash
sudo chmod +x /var/lib/jenkins/alertfly-send.sh
```

> **重要**：不要把脚本内容直接粘贴到 Post Build Task 的 Script 字段。Jenkins 会用 `/bin/sh`（Ubuntu 上是 dash）执行 Script 字段的内容，而 dash 不支持 bash 特有语法（如 `${var//pattern/replacement}`、`$'\n'`、`pipefail`），会导致 `syntax error near unexpected token ')'` 等错误。正确做法是把脚本保存为独立文件，Script 字段用 `bash /var/lib/jenkins/alertfly-send.sh ...` 调用。

---

## 二、Job 配置（核心）

进入需要监控的 Job → **Configure** → **Post-build Actions** → **Add post-build action** → 选择 **Post Build Task**。

**按下表填写 4 个字段：**

| 字段 | 填写值 |
|---|---|
| **Log text** | `marked build as failure` |
| **Script** | `bash /var/lib/jenkins/alertfly-send.sh error "${JOB_NAME} #${BUILD_NUMBER} 构建失败" "URL: ${BUILD_URL}" 5` |
| **Run script only if all previous steps were successful** | ❌ **不勾** |
| **Escalate script execution status to job** | ❌ **不勾** |

点击 **Save** 保存。

**字段说明：**

- **Log text**：正则表达式。Jenkins 在 build step 失败时会自动打印一行 `Build step 'Execute shell' marked build as failure`，只要 console 日志中出现该文本，Script 就会执行。
- **Script**：构建失败时要跑的 shell 命令。这里调用 §1.3 的发送脚本，传入 `level=error`、自定义 title、content 和日志行数（最后一个参数 `5`，可省略，也可用环境变量 `ALERTFLY_LOG_LINES` 指定）。脚本会自动在 content 末尾追加 **`Performing Post build task...` 标记行之前的最后 N 行构建日志**（不含 post-build 自身输出）：优先读 `${JENKINS_HOME}/jobs/<JOB_NAME>/builds/<BUILD_NUMBER>/log`（适用于 Jenkins master 与构建节点为同一台机器的情况），读不到则降级用 `curl ${BUILD_URL}consoleText` 拉取；两者都失败或日志中找不到标记行时，降级为直接取文件末尾 N 行 / content 只保留 URL，不影响报警发送。
- **Run only if all previous steps were successful**：**不要勾**。勾了就变成"只在成功时执行"，与需求相反。
- **Escalate script execution status to job**：**不要勾**。勾了会导致 Redis 短暂不可达时把 job 结果升级为 FAILURE，污染构建状态。

---

## 三、AlertFly 端配置

`config.yaml` 确保订阅到 Jenkins 发的 channel：

```yaml
redis:
  enabled: true
  addr: "192.168.1.100:6379"       # 与脚本中 REDIS_HOST 一致
  mode: "pubsub"
  channel: "alert:*:jenkins:*:*"    # 订阅所有 Jenkins 消息

filter:
  senders:
    - jenkins                        # 只允许 jenkins 弹窗；留空则不过滤
```

---

## 四、验证

1. 在 Job 的 Build Step 里加一行 `exit 1`（或用其他方式让构建失败），触发一次构建
2. 打开 Jenkins **Console Output**，末尾应能看到：
   ```
   Build step 'Execute shell' marked build as failure
   [POSTBUILD] Executing post build task...
   ```
3. AlertFly 端桌面弹出通知；Web UI（默认 `http://127.0.0.1:18080`）历史列表中能看到该消息

---

## 五、常见问题

**Q1：构建失败了但 AlertFly 没收到消息**

按顺序排查：

1. Jenkins Console Output 里搜 `marked build as failure` — 确认这行日志存在（否则 Log text 匹配不到）
2. Jenkins 节点上手动跑一次脚本，看是否能发消息：
   ```bash
   bash /var/lib/jenkins/alertfly-send.sh error "手动测试" "test"
   ```
3. 另开一个终端旁路监听 Redis，验证消息是否到达：
   ```bash
   redis-cli -h 192.168.1.100 SUBSCRIBE 'alert:*:jenkins:*:*'
   ```
4. 检查 AlertFly 端 `redis.channel` 通配符、`filter.senders` 是否包含 `jenkins`

**Q2：想在通知里加更多信息（Git commit、分支、触发人）**

把 Script 改成多行 content：

```bash
bash /var/lib/jenkins/alertfly-send.sh error \
  "${JOB_NAME} #${BUILD_NUMBER} 构建失败" \
  "URL: ${BUILD_URL}
Branch: ${GIT_BRANCH:-N/A}
Commit: ${GIT_COMMIT:-N/A}
Node: ${NODE_NAME}"
```

如需 `${BUILD_USER}`（触发人），额外安装 [Build User Vars Plugin](https://plugins.jenkins.io/build-user-vars-plugin/)。

**Q3：想区分不同项目/环境（改 mission 或 subtype）**

在 Script 前加环境变量覆盖默认值：

```bash
ALERTFLY_MISSION=deploy ALERTFLY_SUBTYPE=backend \
  bash /var/lib/jenkins/alertfly-send.sh error "${JOB_NAME} 部署失败" "URL: ${BUILD_URL}"
```

对应 channel 会变成 `alert:deploy:jenkins:backend:error`。

**Q4：Windows Jenkins 节点**

Post Build Task 在 Windows 节点上执行 batch。把 §1.3 脚本改写为 `.bat`（参考项目内 `scripts/send_alert.bat`），Script 字段调用 `C:\jenkins\alertfly-send.bat error`。

**Q5：报错 `syntax error near unexpected token ')'` 或类似语法错误**

原因：Jenkins Post Build Task 的 Script 字段默认用 `/bin/sh`（Ubuntu 上是 dash）执行，而 dash 不支持 bash 特有语法。

排查步骤：

1. 确认 Script 字段是调用外部脚本文件（`bash /var/lib/jenkins/alertfly-send.sh ...`），**不是**把脚本内容 inline 粘贴
2. 确认 §1.3 脚本使用 POSIX sh 兼容语法（shebang 为 `#!/bin/sh`，无 `${var//pat/rep}`、无 `$'\n'`、无 `pipefail`）
3. 在 Jenkins 节点上手动验证脚本：
   ```bash
   sh /var/lib/jenkins/alertfly-send.sh error "测试" "内容"
   bash /var/lib/jenkins/alertfly-send.sh error "测试" "内容"
   ```
   两种调用都应成功（脚本是 sh 兼容的）
4. 如果必须 inline 到 Script 字段，确保只用 POSIX sh 语法，或显式指定 bash：
   ```
   #!/bin/bash
   # ... bash 脚本内容 ...
   ```
   但 Jenkins 可能忽略 inline 的 shebang，所以**强烈建议用外部脚本文件**。

**Q6：报错 `DENIED Redis is running in protected mode...`**

原因：Redis 默认启用 protected-mode，当未配置 `bind` 地址且无密码时，仅允许 localhost 连接。Jenkins 节点从远程连接会被拒绝。

解决方案（三选一，按推荐度排序）：

**方案 A（推荐生产）：配置 requirepass 密码**

```bash
# 1. 编辑 redis.conf
sudo vim /etc/redis/redis.conf
# 找到 # requirepass foobared，取消注释并改为：
requirepass your_strong_password

# 2. 重启 Redis
sudo systemctl restart redis

# 3. 验证（从 Jenkins 节点）
redis-cli -h 192.168.1.100 -p 6379 -a your_strong_password PING
```

同步修改 §1.3 脚本，在开头加密码变量，并在 redis-cli 命令加 `-a` 参数：

```bash
REDIS_PASSWORD="${ALERTFLY_REDIS_PASSWORD:-}"
# ...
redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" -a "$REDIS_PASSWORD" PUBLISH "$CHANNEL" "$PAYLOAD"
```

AlertFly 端 config.yaml 也要同步加密码：

```yaml
redis:
  addr: "192.168.1.100:6379"
  password: "your_strong_password"
```

**方案 B（推荐内网）：配置 bind 内网 IP**

```bash
# 1. 编辑 redis.conf
sudo vim /etc/redis/redis.conf
# 找到 bind 127.0.0.1 ::1，改为：
bind 127.0.0.1 192.168.1.100

# 2. 重启 Redis
sudo systemctl restart redis

# 3. 验证
redis-cli -h 192.168.1.100 -p 6379 PING
```

**方案 C（仅测试环境）：禁用 protected-mode**

```bash
# 临时生效（从 localhost 执行）
redis-cli CONFIG SET protected-mode no

# 永久生效：编辑 redis.conf
protected-mode no
```

> ⚠️ 禁用 protected-mode 会让 Redis 暴露到网络，生产环境务必配合防火墙或方案 A/B 使用。

---

## 六、参考资料

- [AlertFly 对接接口说明](./integration-guide.md) — 消息格式、channel 命名规范
- [Post Build Task Plugin](https://plugins.jenkins.io/postbuild-task/)
- [redis-cli 命令手册](https://redis.io/docs/ui/cli/)
