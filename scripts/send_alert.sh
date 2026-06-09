#!/usr/bin/env bash
#
# send_alert.sh — 手动发送报警消息到 AlertFly (Linux/macOS)
#
# 用法:
#   交互式:     ./send_alert.sh
#   命令行:     ./send_alert.sh "标题" "内容"
#   覆盖 level: ./send_alert.sh "标题" "内容" error
#

set -euo pipefail

# ── 脚本所在目录 ──
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONF_FILE="${SCRIPT_DIR}/send_alert.user"

# ── 颜色定义 ──
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# ── 检查配置文件 ──
if [[ ! -f "$CONF_FILE" ]]; then
    echo -e "${RED}错误: 配置文件不存在: ${CONF_FILE}${NC}"
    echo -e "${YELLOW}请先复制模板并修改:${NC}"
    echo "  cp ${SCRIPT_DIR}/send_alert.user.example ${CONF_FILE}"
    exit 1
fi

# ── 读取配置（跳过注释和空行）──
declare -A CONF
while IFS='=' read -r key value; do
    # 跳过注释和空行
    [[ -z "$key" || "$key" =~ ^[[:space:]]*# ]] && continue
    # 去除首尾空白
    key="$(echo "$key" | xargs)"
    value="$(echo "$value" | xargs)"
    CONF["$key"]="$value"
done < "$CONF_FILE"

# ── 配置变量（带默认值）──
REDIS_ADDR="${CONF[REDIS_ADDR]:-localhost:6379}"
REDIS_PASSWORD="${CONF[REDIS_PASSWORD]:-}"
REDIS_DB="${CONF[REDIS_DB]:-0}"
CHANNEL="${CONF[CHANNEL]:-alerts}"
MODE="${CONF[MODE]:-pubsub}"
LEVEL="${CONF[LEVEL]:-warn}"
SUBTYPE="${CONF[SUBTYPE]:-manual}"
SENDER="${CONF[SENDER]:-manual}"
MISSION="${CONF[MISSION]:-}"
SOURCE="${CONF[SOURCE]:-redis}"

# ── 检查 redis-cli ──
if ! command -v redis-cli &> /dev/null; then
    echo -e "${RED}错误: redis-cli 未安装${NC}"
    echo ""
    echo "安装方式:"
    echo "  Ubuntu/Debian: sudo apt install redis-tools"
    echo "  CentOS/RHEL:   sudo yum install redis"
    echo "  macOS:         brew install redis"
    exit 1
fi

# ── 获取输入 ──
TITLE=""
CONTENT=""
OVERRIDE_LEVEL=""

if [[ $# -ge 2 ]]; then
    TITLE="$1"
    CONTENT="$2"
    [[ $# -ge 3 ]] && OVERRIDE_LEVEL="$3"
elif [[ $# -eq 1 ]]; then
    echo -e "${YELLOW}用法: $0 \"标题\" \"内容\" [level]${NC}"
    echo ""
    echo "level 可选值: info, warn, error (默认: ${LEVEL})"
    exit 1
else
    # 交互式输入
    echo -e "${GREEN}=== AlertFly 手动报警发送 ===${NC}"
    echo ""
    read -rp "请输入标题 (title): " TITLE
    read -rp "请输入内容 (content): " CONTENT
    read -rp "覆盖级别 (level，回车使用默认 ${LEVEL}): " OVERRIDE_LEVEL
    echo ""
fi

# 验证必填字段
if [[ -z "$TITLE" ]]; then
    echo -e "${RED}错误: 标题不能为空${NC}"
    exit 1
fi
if [[ -z "$CONTENT" ]]; then
    echo -e "${RED}错误: 内容不能为空${NC}"
    exit 1
fi

# 使用覆盖 level 或默认 level
FINAL_LEVEL="${OVERRIDE_LEVEL:-$LEVEL}"

# 验证 level 值
case "$FINAL_LEVEL" in
    info|warn|error) ;;
    *)
        echo -e "${RED}错误: level 必须是 info, warn, error 之一，当前: ${FINAL_LEVEL}${NC}"
        exit 1
        ;;
esac

# ── JSON 转义函数 ──
json_escape() {
    local str="$1"
    # 转义反斜杠
    str="${str//\\/\\\\}"
    # 转义双引号
    str="${str//\"/\\\"}"
    # 转义换行
    str="${str//$'\n'/\\n}"
    # 转义回车
    str="${str//$'\r'/\\r}"
    # 转义制表符
    str="${str//$'\t'/\\t}"
    echo -n "$str"
}

# ── 组装 JSON ──
ESC_TITLE="$(json_escape "$TITLE")"
ESC_CONTENT="$(json_escape "$CONTENT")"
ESC_MISSION="$(json_escape "$MISSION")"
ESC_SENDER="$(json_escape "$SENDER")"

JSON_PAYLOAD="{\"source\":\"${SOURCE}\",\"topic\":\"${CHANNEL}\",\"level\":\"${FINAL_LEVEL}\",\"subtype\":\"${SUBTYPE}\",\"title\":\"${ESC_TITLE}\",\"mission\":\"${ESC_MISSION}\",\"sender\":\"${ESC_SENDER}\",\"content\":\"${ESC_CONTENT}\"}"

# ── 发送消息 ──
echo -e "${GREEN}正在发送报警...${NC}"
echo "  Channel: ${CHANNEL}"
echo "  Level:   ${FINAL_LEVEL}"
echo "  Title:   ${TITLE}"
echo ""

# 构建 redis-cli 命令
REDIS_CMD="redis-cli -h $(echo "$REDIS_ADDR" | cut -d: -f1) -p $(echo "$REDIS_ADDR" | cut -d: -f2) -n ${REDIS_DB}"

# 如果有密码，添加认证参数
if [[ -n "$REDIS_PASSWORD" ]]; then
    REDIS_CMD="${REDIS_CMD} -a ${REDIS_PASSWORD}"
fi

# 执行 PUBLISH
RESULT=$($REDIS_CMD PUBLISH "$CHANNEL" "$JSON_PAYLOAD" 2>&1) || {
    echo -e "${RED}发送失败!${NC}"
    echo "  redis-cli 输出: $RESULT"
    exit 1
}

# 检查返回结果（订阅者数量）
if [[ "$RESULT" =~ ^[0-9]+$ ]]; then
    echo -e "${GREEN}✓ 发送成功!${NC} (订阅者: ${RESULT})"
else
    echo -e "${YELLOW}⚠ 发送完成，但返回异常: ${RESULT}${NC}"
fi
