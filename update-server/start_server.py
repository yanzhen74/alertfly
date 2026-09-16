#!/usr/bin/env python3
"""
AlertFly 服务端 - 自更新文件服务 + HTTP Webhook 报警网关

功能：
  1. 静态文件服务：托管 AlertFly 客户端二进制和 version.json（自更新）
  2. Webhook 网关：接收 HTTP POST 报警，转换为 Redis PUBLISH 发送到四段式话题

话题格式：alert:{mission}:{sender}:{subtype}:{level}

用法：
  python3 start_server.py                          # 使用默认配置启动
  python3 start_server.py --redis-host 10.0.0.5    # 指定 Redis 地址
  python3 start_server.py --token mysecret         # 启用 Token 鉴权
  python3 start_server.py --port 9000              # 自定义端口

依赖：仅 Python3 标准库（无第三方依赖）
"""

import http.server
import socketserver
import json
import socket
import os
import sys
import time
import argparse
import threading
from urllib.parse import urlparse, parse_qs

# ═══════════════════════════════════════════════════════════════
# 配置（通过命令行参数覆盖）
# ═══════════════════════════════════════════════════════════════
CONFIG = {
    "port": 8000,
    "redis_host": "localhost",
    "redis_port": 6379,
    "redis_password": "",
    "redis_db": 0,
    "webhook_token": "",       # 空=不校验 Token
    "webhook_path": "/api/webhook",
}

# ═══════════════════════════════════════════════════════════════
# 最小 Redis 客户端（原生 socket 实现 RESP 协议，无第三方依赖）
# ═══════════════════════════════════════════════════════════════
class RedisClient:
    """线程安全的最小 Redis 客户端，仅实现 AUTH / SELECT / PUBLISH"""

    def __init__(self, host, port, password="", db=0):
        self.host = host
        self.port = port
        self.password = password
        self.db = db
        self._sock = None
        self._lock = threading.Lock()

    def _connect(self):
        """建立连接并执行 AUTH + SELECT"""
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(5)
        s.connect((self.host, self.port))
        self._sock = s

        if self.password:
            self._send_command("AUTH", self.password)
            resp = self._read_response()
            if resp != b"OK":
                raise ConnectionError("Redis AUTH 失败: %s" % resp.decode(errors="replace"))

        if self.db != 0:
            self._send_command("SELECT", str(self.db))
            resp = self._read_response()
            if resp != b"OK":
                raise ConnectionError("Redis SELECT 失败: %s" % resp.decode(errors="replace"))

    def _send_command(self, *args):
        """发送 RESP 命令"""
        parts = ["*%d\r\n" % len(args)]
        for arg in args:
            arg_bytes = str(arg).encode("utf-8")
            parts.append("$%d\r\n%s\r\n" % (len(arg_bytes), arg_bytes.decode("utf-8")))
        self._sock.sendall("".join(parts).encode("utf-8"))

    def _read_response(self):
        """读取 RESP 响应（简化解析）"""
        buf = b""
        while not buf.endswith(b"\r\n"):
            chunk = self._sock.recv(4096)
            if not chunk:
                raise ConnectionError("Redis 连接已断开")
            buf += chunk

        line = buf.decode("utf-8", errors="replace").strip()
        if line.startswith("+"):
            return line[1:].encode()
        elif line.startswith(":"):
            return int(line[1:])
        elif line.startswith("-"):
            raise ConnectionError("Redis 错误: %s" % line[1:])
        elif line.startswith("$"):
            # Bulk string - 需要继续读取数据
            length = int(line[1:])
            if length == -1:
                return None
            data = b""
            while len(data) < length + 2:
                data += self._sock.recv(4096)
            return data[:length]
        return line.encode()

    def publish(self, channel, message):
        """PUBLISH 命令，返回收到消息的订阅者数量"""
        with self._lock:
            try:
                if self._sock is None:
                    self._connect()
                self._send_command("PUBLISH", channel, message)
                result = self._read_response()
                return int(result) if result is not None else 0
            except (ConnectionError, OSError, socket.error) as e:
                # 连接异常，重置后重试一次
                self._close_socket()
                try:
                    self._connect()
                    self._send_command("PUBLISH", channel, message)
                    result = self._read_response()
                    return int(result) if result is not None else 0
                except Exception as retry_err:
                    self._close_socket()
                    raise ConnectionError("Redis PUBLISH 失败: %s (重试: %s)" % (e, retry_err))

    def _close_socket(self):
        """安全关闭 socket"""
        if self._sock:
            try:
                self._sock.close()
            except Exception:
                pass
            self._sock = None

    def close(self):
        """关闭连接"""
        with self._lock:
            self._close_socket()


# 全局 Redis 客户端实例
redis_client = None

# ═══════════════════════════════════════════════════════════════
# 自定义 HTTP 请求处理器
# ═══════════════════════════════════════════════════════════════
class AlertFlyHandler(http.server.SimpleHTTPRequestHandler):
    """扩展处理器：静态文件 + Webhook API"""

    def do_POST(self):
        parsed = urlparse(self.path)
        if parsed.path == CONFIG["webhook_path"]:
            self._handle_webhook(parsed)
        else:
            self._json_response(404, {"code": 1, "msg": "Not Found"})

    def do_GET(self):
        parsed = urlparse(self.path)
        # Webhook GET 返回使用说明
        if parsed.path == CONFIG["webhook_path"]:
            self._json_response(200, {
                "code": 0,
                "msg": "AlertFly Webhook 入口",
                "method": "POST",
                "content_type": "application/json",
                "fields": {
                    "title": "报警标题（必填，与 content 至少填一个）",
                    "content": "报警内容",
                    "level": "报警级别: info/warn/error/critical（默认 warn）",
                    "mission": "任务名称（默认 general）",
                    "sender": "发送者（默认 webhook）",
                    "subtype": "报警子类型（默认 alert）",
                },
                "auth": "X-Webhook-Token 请求头或 ?token= 参数" if CONFIG["webhook_token"] else "无",
                "topic_format": "alert:{mission}:{sender}:{subtype}:{level}",
            })
            return
        # 其余 GET 走静态文件服务
        super().do_GET()

    def _handle_webhook(self, parsed):
        """处理 Webhook POST 请求"""
        # --- Token 鉴权 ---
        if CONFIG["webhook_token"]:
            req_token = self.headers.get("X-Webhook-Token", "")
            if not req_token:
                qs = parse_qs(parsed.query)
                req_token = qs.get("token", [""])[0]
            if req_token != CONFIG["webhook_token"]:
                self._json_response(401, {"code": 1, "msg": "Token 验证失败"})
                return

        # --- 读取请求体 ---
        content_length = int(self.headers.get("Content-Length", 0))
        if content_length == 0:
            self._json_response(400, {"code": 1, "msg": "请求体为空"})
            return

        try:
            body = self.rfile.read(content_length)
            data = json.loads(body.decode("utf-8"))
        except (json.JSONDecodeError, UnicodeDecodeError) as e:
            self._json_response(400, {"code": 1, "msg": "JSON 解析失败: %s" % str(e)})
            return

        if not isinstance(data, dict):
            self._json_response(400, {"code": 1, "msg": "请求体必须是 JSON 对象"})
            return

        # --- 字段提取与默认值 ---
        title = data.get("title", "")
        content = data.get("content", "")
        if not title and not content:
            self._json_response(400, {"code": 1, "msg": "消息必须包含 title 或 content 字段"})
            return

        level = data.get("level", "warn").lower()
        mission = data.get("mission", "general")
        sender = data.get("sender", "webhook")
        subtype = data.get("subtype", "alert")

        # level 校验
        valid_levels = ("info", "warn", "warning", "error", "critical")
        if level not in valid_levels:
            level = "warn"

        # --- 构建 Redis 话题（四段式） ---
        channel = "alert:%s:%s:%s:%s" % (mission, sender, subtype, level)

        # --- 构建消息体 ---
        message = {
            "source": "webhook",
            "topic": channel,
            "level": level,
            "title": title or content[:50],
            "content": content or title,
            "mission": mission,
            "sender": sender,
            "subtype": subtype,
            "received_at": time.strftime("%Y-%m-%dT%H:%M:%S+08:00"),
        }
        # 透传额外字段（如 id）
        if "id" in data:
            message["id"] = data["id"]

        payload = json.dumps(message, ensure_ascii=False)

        # --- PUBLISH 到 Redis ---
        try:
            subscribers = redis_client.publish(channel, payload)
        except ConnectionError as e:
            print("[ERROR] Redis PUBLISH 失败: %s" % e)
            self._json_response(502, {"code": 1, "msg": "Redis 发布失败: %s" % str(e)})
            return

        print("[WEBHOOK] %s -> %s (订阅者: %d)" % (
            time.strftime("%H:%M:%S"), channel, subscribers))

        self._json_response(200, {
            "code": 0,
            "msg": "ok",
            "channel": channel,
            "subscribers": subscribers,
        })

    def _json_response(self, status_code, data):
        """发送 JSON 响应"""
        body = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(status_code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        """自定义日志格式，过滤静态文件请求的冗余输出"""
        path = args[0].split(" ")[1] if args else ""
        # 静态文件 GET 请求不打印（减少噪音），仅打印 API 和非 200 请求
        if path == CONFIG["webhook_path"] or "404" in str(args) or "500" in str(args):
            print("[%s] %s" % (time.strftime("%H:%M:%S"), format % args))


# ═══════════════════════════════════════════════════════════════
# 启动入口
# ═══════════════════════════════════════════════════════════════
class ThreadedTCPServer(socketserver.ThreadingMixIn, socketserver.TCPServer):
    """多线程 TCP 服务器，支持并发请求"""
    allow_reuse_address = True
    daemon_threads = True


def main():
    global redis_client

    parser = argparse.ArgumentParser(
        description="AlertFly 服务端（自更新 + Webhook 报警网关）")
    parser.add_argument("--port", type=int, default=CONFIG["port"],
                        help="HTTP 监听端口（默认 8000）")
    parser.add_argument("--redis-host", default=CONFIG["redis_host"],
                        help="Redis 服务地址（默认 localhost）")
    parser.add_argument("--redis-port", type=int, default=CONFIG["redis_port"],
                        help="Redis 端口（默认 6379）")
    parser.add_argument("--redis-password", default=CONFIG["redis_password"],
                        help="Redis 密码（默认空）")
    parser.add_argument("--redis-db", type=int, default=CONFIG["redis_db"],
                        help="Redis 数据库编号（默认 0）")
    parser.add_argument("--token", default=CONFIG["webhook_token"],
                        help="Webhook 鉴权 Token（默认空=不校验）")
    parser.add_argument("--webhook-path", default=CONFIG["webhook_path"],
                        help="Webhook 路由路径（默认 /api/webhook）")
    args = parser.parse_args()

    # 更新配置
    CONFIG["port"] = args.port
    CONFIG["redis_host"] = args.redis_host
    CONFIG["redis_port"] = args.redis_port
    CONFIG["redis_password"] = args.redis_password
    CONFIG["redis_db"] = args.redis_db
    CONFIG["webhook_token"] = args.token
    CONFIG["webhook_path"] = args.webhook_path

    # 切换工作目录到脚本所在目录（静态文件服务需要）
    os.chdir(os.path.dirname(os.path.abspath(__file__)))

    # 初始化 Redis 客户端
    redis_client = RedisClient(
        host=CONFIG["redis_host"],
        port=CONFIG["redis_port"],
        password=CONFIG["redis_password"],
        db=CONFIG["redis_db"],
    )

    # 启动服务器
    server = ThreadedTCPServer(("0.0.0.0", CONFIG["port"]), AlertFlyHandler)

    print("=" * 55)
    print("  AlertFly 服务端已启动")
    print("=" * 55)
    print("  监听地址:    http://0.0.0.0:%d" % CONFIG["port"])
    print("  自更新服务:  http://<IP>:%d/version.json" % CONFIG["port"])
    print("  Webhook 入口: POST http://<IP>:%d%s" % (CONFIG["port"], CONFIG["webhook_path"]))
    print("  Redis 目标:  %s:%d (db%d)" % (CONFIG["redis_host"], CONFIG["redis_port"], CONFIG["redis_db"]))
    if CONFIG["webhook_token"]:
        print("  Token 鉴权:  已启用")
    else:
        print("  Token 鉴权:  未启用（所有请求均可推送）")
    print("=" * 55)
    print("")
    print("Webhook 调用示例:")
    print('  curl -X POST http://<IP>:%d%s \\' % (CONFIG["port"], CONFIG["webhook_path"]))
    print('    -H "Content-Type: application/json" \\')
    print('    -d \'{"title":"部署失败","content":"生产环境超时","level":"error","mission":"deploy","sender":"jenkins"}\'')
    print("")

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\n[INFO] 收到中断信号，正在关闭...")
        server.shutdown()
        redis_client.close()
        print("[INFO] 服务已停止")


if __name__ == "__main__":
    main()
