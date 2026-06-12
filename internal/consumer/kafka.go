package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
)

// KafkaConsumer 基于 sarama.ConsumerGroup 的 Kafka 消费者实现
type KafkaConsumer struct {
	cfg               *config.KafkaConfig
	client            sarama.ConsumerGroup
	brokers           sarama.Client // 用于调用 Topics() 获取所有 topic 列表
	handler           *consumerGroupHandler
	msgs              chan *model.Message
	errs              chan error
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	resolvedTopics    []string
	lastScanTime      time.Time
	topicScanInterval time.Duration
}

// consumerGroupHandler 实现 sarama.ConsumerGroupHandler 接口
type consumerGroupHandler struct {
	msgs chan *model.Message
}

// NewKafkaConsumer 创建新的 Kafka 消费者实例
func NewKafkaConsumer(cfg *config.KafkaConfig) (*KafkaConsumer, error) {
	saramaCfg := sarama.NewConfig()

	// 解析 Kafka 版本
	kafkaVersion := cfg.Version
	if kafkaVersion == "" {
		kafkaVersion = "2.0.0"
	}
	version, err := sarama.ParseKafkaVersion(kafkaVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid kafka version %q: %w", kafkaVersion, err)
	}
	saramaCfg.Version = version
	log.Printf("[kafka] 使用 Kafka 协议版本: %s", kafkaVersion)

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

	// 创建底层 Client 用于获取 topic 列表（正则匹配需要）
	brokerClient, err := sarama.NewClient(cfg.Brokers, saramaCfg)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create sarama client for topic discovery failed: %w", err)
	}

	msgs := make(chan *model.Message, 256)
	errs := make(chan error, 64)

	handler := &consumerGroupHandler{
		msgs: msgs,
	}

	interval := cfg.TopicScanInterval
	if interval <= 0 {
		interval = 30
	}

	return &KafkaConsumer{
		cfg:               cfg,
		client:            client,
		brokers:           brokerClient,
		handler:           handler,
		msgs:              msgs,
		errs:              errs,
		topicScanInterval: time.Duration(interval) * time.Second,
	}, nil
}

// getTopics 带缓存的 topic 解析，按配置间隔定期刷新
func (k *KafkaConsumer) getTopics() ([]string, error) {
	now := time.Now()
	// 有缓存且未过期，直接返回
	if len(k.resolvedTopics) > 0 && now.Sub(k.lastScanTime) < k.topicScanInterval {
		return k.resolvedTopics, nil
	}

	isFirst := len(k.resolvedTopics) == 0

	topics, err := k.resolveTopics()
	if err != nil {
		// 解析失败但有旧缓存，沿用旧缓存
		if len(k.resolvedTopics) > 0 {
			log.Printf("[kafka] topic 扫描失败，使用缓存: %v", err)
			return k.resolvedTopics, nil
		}
		return nil, err
	}

	if isFirst {
		log.Printf("[kafka] 首次扫描 topics，发现 %d 个匹配: %v", len(topics), topics)
	} else {
		// 对比新旧，仅在有变化时输出日志
		oldSet := make(map[string]bool, len(k.resolvedTopics))
		for _, t := range k.resolvedTopics {
			oldSet[t] = true
		}
		newSet := make(map[string]bool, len(topics))
		for _, t := range topics {
			newSet[t] = true
		}
		var added, removed []string
		for _, t := range topics {
			if !oldSet[t] {
				added = append(added, t)
			}
		}
		for _, t := range k.resolvedTopics {
			if !newSet[t] {
				removed = append(removed, t)
			}
		}
		if len(added) > 0 || len(removed) > 0 {
			log.Printf("[kafka] topic 扫描更新：新增 %v，移除 %v", added, removed)
		}
	}

	k.resolvedTopics = topics
	k.lastScanTime = now
	return topics, nil
}

// resolveTopics 解析配置中的 topics 列表，将精确 topic 和正则匹配的 topic 合并
// 精确 topic 直接保留，带 "regex:" 前缀的通过正则包含匹配，带 "!regex:" 前缀的通过正则排除匹配
// 处理顺序：先包含匹配，再排除过滤；精确 topic 不受排除规则影响
func (k *KafkaConsumer) resolveTopics() ([]string, error) {
	var exactTopics []string
	var includePatterns []struct {
		raw string // 原始正则字符串，用于日志
		re  *regexp.Regexp
	}
	var excludePatterns []struct {
		raw string
		re  *regexp.Regexp
	}

	for _, t := range k.cfg.Topics {
		if strings.HasPrefix(t, "!regex:") {
			pattern := strings.TrimPrefix(t, "!regex:")
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid exclude regex %q: %w", pattern, err)
			}
			excludePatterns = append(excludePatterns, struct {
				raw string
				re  *regexp.Regexp
			}{raw: pattern, re: re})
		} else if strings.HasPrefix(t, "regex:") {
			pattern := strings.TrimPrefix(t, "regex:")
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid topic regex %q: %w", pattern, err)
			}
			includePatterns = append(includePatterns, struct {
				raw string
				re  *regexp.Regexp
			}{raw: pattern, re: re})
		} else {
			exactTopics = append(exactTopics, t)
		}
	}

	// 没有正则模式，直接返回精确 topic
	if len(includePatterns) == 0 && len(excludePatterns) == 0 {
		log.Printf("[kafka] 精确订阅 topics: %v", exactTopics)
		return exactTopics, nil
	}

	// 获取 broker 上所有 topic 并正则匹配
	allTopics, err := k.brokers.Topics()
	if err != nil {
		return nil, fmt.Errorf("failed to get topics from broker: %w", err)
	}

	// 记录精确 topic
	if len(exactTopics) > 0 {
		log.Printf("[kafka] 精确订阅 topics: %v", exactTopics)
	}

	// 先用包含规则匹配；无包含规则时，排除规则作用于所有 broker topic
	resolvedSet := make(map[string]bool) // 去重
	var matched []string

	if len(includePatterns) > 0 {
		for _, p := range includePatterns {
			var hits []string
			for _, topic := range allTopics {
				if p.re.MatchString(topic) && !resolvedSet[topic] {
					hits = append(hits, topic)
					resolvedSet[topic] = true
				}
			}
			log.Printf("[kafka] 正则包含 %q → 匹配 %d 个 topic", p.raw, len(hits))
			matched = append(matched, hits...)
		}
	} else {
		// 仅有排除规则：以所有 broker topic 为候选集
		for _, topic := range allTopics {
			if !resolvedSet[topic] {
				matched = append(matched, topic)
				resolvedSet[topic] = true
			}
		}
	}

	// 再用排除规则过滤
	if len(excludePatterns) > 0 {
		var filtered []string
		for _, p := range excludePatterns {
			excludeCount := 0
			var remaining []string
			for _, topic := range matched {
				if p.re.MatchString(topic) {
					excludeCount++
				} else {
					remaining = append(remaining, topic)
				}
			}
			log.Printf("[kafka] 正则排除 %q → 排除 %d 个 topic", p.raw, excludeCount)
			matched = remaining
		}
		filtered = matched
		matched = filtered
	}

	// 合并精确 topic 和正则匹配的 topic
	result := make([]string, 0, len(exactTopics)+len(matched))
	result = append(result, exactTopics...)
	result = append(result, matched...)

	log.Printf("[kafka] 最终消费 topics (%d个): %v", len(result), result)

	return result, nil
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

			// 定期扫描 topics（带缓存，按 topicScanInterval 刷新）
			topics, err := k.getTopics()
			if err != nil {
				k.sendError(fmt.Errorf("resolve topics failed: %w", err))

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

			if len(topics) == 0 {
				log.Printf("[kafka] 未匹配到任何 topic，等待重试...")
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}

			err = k.client.Consume(ctx, topics, k.handler)
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
	var err error
	if k.brokers != nil {
		if e := k.brokers.Close(); e != nil {
			err = e
		}
	}
	if k.client != nil {
		if e := k.client.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
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

// extractFromTopic 从 Kafka topic 名称中提取元数据
// 格式：alert.{mission}.{sender}.{subtype}.{level}
func extractFromTopic(topic string) (mission, sender, subtype, level string) {
	parts := strings.Split(topic, ".")
	if len(parts) == 5 && parts[0] == "alert" {
		return parts[1], parts[2], parts[3], parts[4]
	}
	return "", "", "", ""
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
				Topic:   msg.Topic,
				Title:   "Raw Message",
				Content: string(msg.Value),
			}
		} else {
			// 确保 Source 和 Topic 字段正确
			message.Source = "kafka"
			message.Topic = msg.Topic
			// 如果 JSON 中未提供 mission/sender/subtype/level，从 topic 名称自动填充
			if message.Mission == "" || message.Sender == "" || message.SubType == "" || message.Level == "" {
				tMission, tSender, tSubtype, tLevel := extractFromTopic(msg.Topic)
				if message.Mission == "" {
					message.Mission = tMission
				}
				if message.Sender == "" {
					message.Sender = tSender
				}
				if message.SubType == "" {
					message.SubType = tSubtype
				}
				if message.Level == "" {
					message.Level = tLevel
				}
			}
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
