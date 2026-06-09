package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
)

// RedisConsumer 基于 Redis 的消息消费者，支持 PubSub 和 Stream 两种模式
type RedisConsumer struct {
	cfg    *config.RedisConfig
	client *redis.Client

	msgCh  chan *model.Message
	errCh  chan error

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

// NewRedisConsumer 创建 Redis 消费者实例
func NewRedisConsumer(cfg *config.RedisConfig) (*RedisConsumer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("redis config is nil")
	}
	if cfg.Mode != "pubsub" && cfg.Mode != "stream" {
		return nil, fmt.Errorf("unsupported redis mode: %s, must be pubsub or stream", cfg.Mode)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	return &RedisConsumer{
		cfg:   cfg,
		client: client,
		msgCh:  make(chan *model.Message, 256),
		errCh:  make(chan error, 16),
	}, nil
}

// Messages 返回消息通道
func (c *RedisConsumer) Messages() <-chan *model.Message {
	return c.msgCh
}

// Errors 返回错误通道
func (c *RedisConsumer) Errors() <-chan error {
	return c.errCh
}

// Start 启动消费者
func (c *RedisConsumer) Start(ctx context.Context) error {
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()

	switch c.cfg.Mode {
	case "pubsub":
		go c.consumePubSub(childCtx)
	case "stream":
		go c.consumeStream(childCtx)
	}

	return nil
}

// Close 优雅关闭消费者
func (c *RedisConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.cancel != nil {
		c.cancel()
	}

	close(c.msgCh)
	close(c.errCh)

	return c.client.Close()
}

// ---------- PubSub 模式 ----------

func (c *RedisConsumer) consumePubSub(ctx context.Context) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sub := c.client.Subscribe(ctx, c.cfg.Channel)
		_, err := sub.Receive(ctx)
		if err != nil {
			c.errCh <- fmt.Errorf("pubsub subscribe error: %w", err)
			sub.Close()
			backoff = c.reconnectBackoff(backoff, ctx)
			continue
		}

		// 重连成功，重置退避
		backoff = time.Second

		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					// 通道关闭，需要重连
					c.errCh <- fmt.Errorf("pubsub channel closed, reconnecting...")
					sub.Close()
					backoff = c.reconnectBackoff(backoff, ctx)
					goto pubsubReconnect
				}
				c.msgCh <- c.parseMessage(msg.Payload, c.cfg.Channel)
			}
		}

	pubsubReconnect:
	}
}

// ---------- Stream 模式 ----------

func (c *RedisConsumer) consumeStream(ctx context.Context) {
	backoff := time.Second

	// 确保消费者组存在
	if err := c.ensureConsumerGroup(ctx); err != nil {
		c.errCh <- fmt.Errorf("stream consumer group init error: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.cfg.ConsumerGroup,
			Consumer: "alertfly",
			Streams:  []string{c.cfg.Stream, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// 没有新消息，正常
				backoff = time.Second
				continue
			}
			if ctx.Err() != nil {
				return
			}
			c.errCh <- fmt.Errorf("stream xreadgroup error: %w", err)
			backoff = c.reconnectBackoff(backoff, ctx)
			continue
		}

		// 重连成功/读取正常，重置退避
		backoff = time.Second

		for _, stream := range streams {
			for _, message := range stream.Messages {
				c.msgCh <- c.parseStreamMessage(message, stream.Stream)
			}
		}
	}
}

// ensureConsumerGroup 确保消费者组存在，不存在则创建
func (c *RedisConsumer) ensureConsumerGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.cfg.Stream, c.cfg.ConsumerGroup, "0").Err()
	if err != nil {
		// BUSYGROUP 错误表示组已存在，忽略
		if strings.Contains(err.Error(), "BUSYGROUP") {
			return nil
		}
		return err
	}
	return nil
}

// ---------- 消息解析 ----------

// parseMessage 将 PubSub 收到的 payload 解析为 Message
func (c *RedisConsumer) parseMessage(payload string, topic string) *model.Message {
	msg := &model.Message{
		Source:     "redis",
		Topic:      topic,
		ReceivedAt: time.Now(),
	}

	if err := json.Unmarshal([]byte(payload), msg); err != nil {
		// JSON 解析失败，将原始数据放入 Content，Title 设为 "Raw Message"
		msg.Title = "Raw Message"
		msg.Content = payload
	}

	return msg
}

// parseStreamMessage 将 Stream 消息解析为 Message
func (c *RedisConsumer) parseStreamMessage(xmsg redis.XMessage, stream string) *model.Message {
	// Stream 消息的 Values 是一个 map[string]interface{}
	// 尝试将整个 Values 序列化为 JSON 再解析
	data, err := json.Marshal(xmsg.Values)
	if err != nil {
		return &model.Message{
			Source:     "redis",
			Topic:      stream,
			Title:      "Raw Message",
			Content:    fmt.Sprintf("%v", xmsg.Values),
			ReceivedAt: time.Now(),
		}
	}

	msg := &model.Message{
		Source:     "redis",
		Topic:      stream,
		ReceivedAt: time.Now(),
	}

	if err := json.Unmarshal(data, msg); err != nil {
		// JSON 解析失败，将原始数据放入 Content
		msg.Title = "Raw Message"
		msg.Content = string(data)
	}

	return msg
}

// ---------- 重连退避 ----------

// reconnectBackoff 执行指数退避等待，最大间隔 30s
func (c *RedisConsumer) reconnectBackoff(current time.Duration, ctx context.Context) time.Duration {
	select {
	case <-ctx.Done():
		return current
	case <-time.After(current):
	}

	// 指数增长，上限 30s
	next := current * 2
	if next > 30*time.Second {
		next = 30 * time.Second
	}
	return next
}
