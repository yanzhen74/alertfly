package consumer

import (
	"context"

	"github.com/oliverxu/alertfly/internal/model"
)

// Consumer 消费者接口，定义统一的消息消费行为
type Consumer interface {
	// Start 启动消费者，开始消费消息
	Start(ctx context.Context) error
	// Messages 返回消息通道
	Messages() <-chan *model.Message
	// Errors 返回错误通道
	Errors() <-chan error
	// Close 优雅关闭消费者
	Close() error
}
