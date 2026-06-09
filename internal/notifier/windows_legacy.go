//go:build windows && win7
// +build windows,win7

package notifier

import (
	"fmt"
	"log"

	"github.com/oliverxu/alertfly/internal/model"
)

// windowsNotifier Windows 7 通知器（需 -tags win7 编译）。
// 使用 Shell_NotifyIconW Balloon Tip 显示通知。
// 实际的通知发送由 trayNotify 回调负责（通过 systray.ShowNotification），
// 此处仅做日志记录。
type windowsNotifier struct{}

// NewNotifier 根据当前平台创建对应的通知器实例
func NewNotifier() Notifier {
	return &windowsNotifier{}
}

// Notify 记录告警日志（气泡通知由 trayNotify 回调处理）
func (n *windowsNotifier) Notify(msg *model.Message) error {
	title := msg.Title
	if title == "" {
		title = fmt.Sprintf("[%s] %s", msg.Level, msg.Source)
	}
	log.Printf("[notifier] [%s] %s: %s", msg.Level, title, truncateLegacy(msg.Content, 200))
	return nil
}

// NotifyError 记录错误日志（气泡通知由 trayNotify 回调处理）
func (n *windowsNotifier) NotifyError(title string, body string) error {
	log.Printf("[notifier] [error] %s: %s", title, body)
	return nil
}

// truncateLegacy 截断字符串，最多保留 maxRunes 个 rune
func truncateLegacy(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
