//go:build windows

package notifier

import (
	"fmt"
	"log"

	"github.com/go-toast/toast"
	"github.com/oliverxu/alertfly/internal/model"
)

// windowsNotifier 使用 Windows Toast Notification 发送系统通知
type windowsNotifier struct{}

// NewNotifier 根据当前平台创建对应的通知器实例
func NewNotifier() Notifier {
	return &windowsNotifier{}
}

// Notify 发送告警通知
func (n *windowsNotifier) Notify(msg *model.Message) error {
	title := msg.Title
	if title == "" {
		title = fmt.Sprintf("[%s] %s", msg.Level, msg.Source)
	}
	body := truncate(msg.Content, 200)

	notification := toast.Notification{
		AppID:   "AlertFly",
		Title:   title,
		Message: body,
	}

	if err := notification.Push(); err != nil {
		log.Printf("[notifier] toast notification failed: %v", err)
		return fmt.Errorf("toast notification failed: %w", err)
	}
	return nil
}

// NotifyError 发送系统错误通知
func (n *windowsNotifier) NotifyError(title string, body string) error {
	notification := toast.Notification{
		AppID:   "AlertFly",
		Title:   title,
		Message: body,
	}

	if err := notification.Push(); err != nil {
		log.Printf("[notifier] toast notification failed: %v", err)
		return fmt.Errorf("toast notification failed: %w", err)
	}
	return nil
}

// truncate 截断字符串，最多保留 maxRunes 个 rune
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
