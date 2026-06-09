//go:build linux

package notifier

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/oliverxu/alertfly/internal/model"
)

// linuxNotifier 使用 notify-send 发送系统通知
type linuxNotifier struct {
	available bool // notify-send 是否可用
}

// NewNotifier 根据当前平台创建对应的通知器实例
func NewNotifier() Notifier {
	n := &linuxNotifier{}
	if _, err := exec.LookPath("notify-send"); err != nil {
		log.Printf("[notifier] notify-send not found in PATH, notifications will be logged only")
		n.available = false
	} else {
		n.available = true
	}
	return n
}

// Notify 发送告警通知
func (n *linuxNotifier) Notify(msg *model.Message) error {
	title := msg.Title
	if title == "" {
		title = fmt.Sprintf("[%s] %s", msg.Level, msg.Source)
	}
	body := truncate(msg.Content, 200)
	urgency := levelToUrgency(msg.Level)

	if !n.available {
		log.Printf("[notifier] (no notify-send) [%s] %s: %s", urgency, title, body)
		return nil
	}

	return n.send(urgency, title, body)
}

// NotifyError 发送系统错误通知
func (n *linuxNotifier) NotifyError(title string, body string) error {
	if !n.available {
		log.Printf("[notifier] (no notify-send) [critical] %s: %s", title, body)
		return nil
	}
	return n.send("critical", title, body)
}

// send 调用 notify-send 发送通知
func (n *linuxNotifier) send(urgency, title, body string) error {
	cmd := exec.Command("notify-send", "-u", urgency, title, body)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[notifier] notify-send failed: %v, output: %s", err, strings.TrimSpace(string(output)))
		return fmt.Errorf("notify-send failed: %w", err)
	}
	return nil
}

// levelToUrgency 将消息等级映射为 notify-send 的紧急程度
func levelToUrgency(level string) string {
	switch strings.ToLower(level) {
	case "error":
		return "critical"
	case "warn":
		return "normal"
	case "info":
		return "low"
	default:
		return "normal"
	}
}

// truncate 截断字符串，最多保留 maxRunes 个 rune
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
