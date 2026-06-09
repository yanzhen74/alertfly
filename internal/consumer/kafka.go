package consumer

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
)

// KafkaConsumer 基于 sarama.ConsumerGroup 的 Kafka 消费者实现
type KafkaConsumer struct {
	cfg     *config.KafkaConfig
	client  sarama.ConsumerGroup
	handler *consumerGroupHandler
	msgs    chan *model.Message
	errs    chan error
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// consumerGroupHandler 实现 sarama.ConsumerGroupHandler 接口
type consumerGroupHandler struct {
	msgs  chan *model.Message
	topic string
}

// NewKafkaConsumer 创建新的 Kafka 消费者实例
func NewKafkaConsumer(cfg *config.KafkaConfig) (*KafkaConsumer, error) {
	saramaCfg := sarama.NewConfig()
	saramaCfg.Version = sarama.V2_0_0_0

	// 启用自动提交 offset
	saramaCfg.Consumer.Offsets.AutoCommit.Enable = true
	saramaCfg.Consumer.Offsets.AutoCommit.Interval = time.Second
	// 从最早的可用消息开始消费
	saramaCfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	// 设置 Rebalance 策略
	saramaCfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{
		sarama.BalanceStrategyRoundRobin,
	}

	client, err := sarama.NewConsumerGroup(cfg.Brokers, cfg.GroupID, saramaCfg)
	if err != nil {
		return nil, err
	}

	msgs := make(chan *model.Message, 256)
	errs := make(chan error, 64)

	handler := &consumerGroupHandler{
		msgs:  msgs,
		topic: cfg.Topic,
	}

	return &KafkaConsumer{
		cfg:     cfg,
		client:  client,
		handler: handler,
		msgs:    msgs,
		errs:    errs,
	}, nil
}

// Start 启动消费者，以 goroutine 方式持续消费消息
func (k *KafkaConsumer) Start(ctx context.Context) error {
	ctx, k.cancel = context.WithCancel(ctx)

	// 转发 sarama 内部错误到 errs 通道
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		for err := range k.client.Errors() {
			k.sendError(err)
		}
	}()

	// 主消费循环，带指数退避重连
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		defer close(k.msgs)
		defer close(k.errs)

		backoff := time.Second
		const maxBackoff = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			err := k.client.Consume(ctx, []string{k.cfg.Topic}, k.handler)
			if err != nil {
				k.sendError(err)

				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					backoff *= 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
				}
				continue
			}

			// 消费正常结束后重置退避
			backoff = time.Second

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()

	return nil
}

// Messages 返回消息通道
func (k *KafkaConsumer) Messages() <-chan *model.Message {
	return k.msgs
}

// Errors 返回错误通道
func (k *KafkaConsumer) Errors() <-chan error {
	return k.errs
}

// Close 优雅关闭消费者
func (k *KafkaConsumer) Close() error {
	if k.cancel != nil {
		k.cancel()
	}
	k.wg.Wait()
	if k.client != nil {
		return k.client.Close()
	}
	return nil
}

// sendError 安全地发送错误到 errs 通道
func (k *KafkaConsumer) sendError(err error) {
	select {
	case k.errs <- err:
	default:
		// errs 通道已满，丢弃旧错误
		select {
		case <-k.errs:
		default:
		}
		k.errs <- err
	}
}

// --- sarama.ConsumerGroupHandler 实现 ---

// Setup 在新的消费会话开始时调用
func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup 在消费会话结束时调用
func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim 处理消费到的消息
func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		message := &model.Message{}
		if err := json.Unmarshal(msg.Value, message); err != nil {
			// JSON 解析失败，将原始数据放入 Content 字段
			message = &model.Message{
				Source:  "kafka",
				Topic:   h.topic,
				Title:   "Raw Message",
				Content: string(msg.Value),
			}
		} else {
			// 确保 Source 和 Topic 字段正确
			message.Source = "kafka"
			message.Topic = h.topic
		}

		// 设置接收时间
		if message.ReceivedAt.IsZero() {
			message.ReceivedAt = time.Now()
		}

		select {
		case h.msgs <- message:
		case <-session.Context().Done():
			return nil
		}

		// 标记消息已处理，用于自动提交 offset
		session.MarkMessage(msg, "")
	}
	return nil
}
