package consumer

import (
	"context"
	"time"

	"github.com/oliverxu/alertfly/internal/model"
)

// ConsumerStatus 消费者连接状态
type ConsumerStatus struct {
	Name      string    `json:"name"`       // "Redis" / "Kafka"
	Enabled   bool      `json:"enabled"`    // 配置是否启用
	Connected bool      `json:"connected"`  // 当前是否已连接
	LastError string    `json:"last_error"` // 最近一次错误信息，空=无错误
	Since     time.Time `json:"since"`      // 当前状态持续时间（连接时间或错误时间）
}

// Consumer 消费者接口，定义统一的消息消费行为
type Consumer interface {
	// Start 启动消费者，开始消费消息
	Start(ctx context.Context) error
	// Messages 返回消息通道
	Messages() <-chan *model.Message
	// Errors 返回错误通道
	Errors() <-chan error
	// Status 返回当前连接状态
	Status() ConsumerStatus
	// Close 优雅关闭消费者
	Close() error
}
