package notifier

import "github.com/oliverxu/alertfly/internal/model"

// Notifier 通知接口
type Notifier interface {
	// Notify 发送一条告警通知（不抢焦点）
	Notify(msg *model.Message) error
	// NotifyError 发送系统错误通知（如连接异常）
	NotifyError(title string, body string) error
}
