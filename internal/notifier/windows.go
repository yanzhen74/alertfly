//go:build windows

package notifier

import (
	"log"

	"github.com/oliverxu/alertfly/internal/model"
)

// windowsNotifier Windows 上不再使用 toast 通知（PowerShell 启动慢），
// 所有通知通过 AsyncNotifier 的 trayNotify 回调走 Balloon Tip。
// 此处仅作为 Notifier 接口的空实现。
type windowsNotifier struct{}

// NewNotifier 根据当前平台创建对应的通知器实例
func NewNotifier() Notifier {
	return &windowsNotifier{}
}

// Notify Windows 上由 trayApp.ShowNotification 统一处理，此处仅记日志
func (n *windowsNotifier) Notify(msg *model.Message) error {
	log.Printf("[notifier] [%s] %s: %s", msg.Level, msg.Title, truncate(msg.Content, 100))
	return nil
}

// NotifyError 系统错误通知也由 tray 处理，此处仅记日志
func (n *windowsNotifier) NotifyError(title string, body string) error {
	log.Printf("[notifier] [error] %s: %s", title, body)
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
