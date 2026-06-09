package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/oliverxu/alertfly/internal/config"
)

const (
	// redisIntegrationAddr 集成测试使用的 Redis 地址
	redisIntegrationAddr = "localhost:6379"
	// redisIntegrationChannel 集成测试使用的 PubSub channel
	redisIntegrationChannel = "alertfly:integration:test"
	// redisIntegrationStream 集成测试使用的 Stream 名称
	redisIntegrationStream = "alertfly:integration:test_stream"
	// redisIntegrationGroup 集成测试使用的消费者组
	redisIntegrationGroup = "alertfly-integration-test-group"
)

// skipIfNoRedis 检测本地 Redis 是否可用，不可用则跳过测试
func skipIfNoRedis(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redisIntegrationAddr})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping integration test: Redis not available at %s: %v", redisIntegrationAddr, err)
	}
}

// cleanupRedisKeys 清理集成测试产生的 Redis key
func cleanupRedisKeys(t *testing.T) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redisIntegrationAddr})
	defer client.Close()
	ctx := context.Background()
	client.Del(ctx, redisIntegrationStream)
	client.XGroupDestroy(ctx, redisIntegrationStream, redisIntegrationGroup)
}

// TestIntegrationPubSub 集成测试：PubSub 模式收发消息
func TestIntegrationPubSub(t *testing.T) {
	skipIfNoRedis(t)

	cfg := &config.RedisConfig{
		Enabled: true,
		Addr:    redisIntegrationAddr,
		Channel: redisIntegrationChannel,
		Mode:    "pubsub",
	}

	consumer, err := NewRedisConsumer(cfg)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}
	defer consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	// 发布测试消息
	publisher := redis.NewClient(&redis.Options{Addr: redisIntegrationAddr})
	defer publisher.Close()

	testMsg := map[string]interface{}{
		"id":      int64(9001),
		"source":  "integration-test",
		"level":   "info",
		"title":   "Integration Test Alert",
		"content": "This is an integration test message",
	}
	data, _ := json.Marshal(testMsg)

	// 等待消费者订阅就绪
	time.Sleep(500 * time.Millisecond)

	result, err := publisher.Publish(ctx, redisIntegrationChannel, string(data)).Result()
	if err != nil {
		t.Fatalf("failed to publish message: %v", err)
	}
	t.Logf("published message to %d subscriber(s)", result)

	// 等待接收消息
	select {
	case msg := <-consumer.Messages():
		t.Logf("received message: id=%d level=%s title=%s", msg.ID, msg.Level, msg.Title)
		// 注意：JSON 中的 source 字段会覆盖 parseMessage 设置的默认 "redis" 值
		if msg.Source != "integration-test" {
			t.Errorf("expected Source=integration-test (from JSON), got %s", msg.Source)
		}
		if msg.Level != "info" {
			t.Errorf("expected Level=info, got %s", msg.Level)
		}
		if msg.Title != "Integration Test Alert" {
			t.Errorf("expected Title='Integration Test Alert', got %s", msg.Title)
		}
	case err := <-consumer.Errors():
		t.Fatalf("received error from consumer: %v", err)
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}
}

// TestIntegrationStream 集成测试：Stream 模式收发消息
func TestIntegrationStream(t *testing.T) {
	skipIfNoRedis(t)
	cleanupRedisKeys(t)

	cfg := &config.RedisConfig{
		Enabled:       true,
		Addr:          redisIntegrationAddr,
		Stream:        redisIntegrationStream,
		ConsumerGroup: redisIntegrationGroup,
		Mode:          "stream",
	}

	consumer, err := NewRedisConsumer(cfg)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}
	defer consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	// 写入 Stream 消息
	publisher := redis.NewClient(&redis.Options{Addr: redisIntegrationAddr})
	defer publisher.Close()

	streamValues := map[string]interface{}{
		"id":      "9002",
		"source":  "integration-test",
		"level":   "error",
		"title":   "Stream Integration Test",
		"content": "This is a stream integration test message",
	}

	streamID, err := publisher.XAdd(ctx, &redis.XAddArgs{
		Stream: redisIntegrationStream,
		Values: streamValues,
	}).Result()
	if err != nil {
		t.Fatalf("failed to XAdd message: %v", err)
	}
	t.Logf("added message to stream, ID=%s", streamID)

	// 等待接收消息
	select {
	case msg := <-consumer.Messages():
		t.Logf("received stream message: level=%s title=%s", msg.Level, msg.Title)
	// 注意：Stream 消息中所有值都是 string 类型，id 为字符串会导致 json.Unmarshal 失败，
	// 走 fallback 分支。同时 source 字段被部分 unmarshal 覆盖。
	if msg.Source != "integration-test" {
		t.Errorf("expected Source=integration-test (partially unmarshaled), got %s", msg.Source)
	}
	if msg.Level != "error" {
		t.Errorf("expected Level=error, got %s", msg.Level)
	}
	if msg.Title != "Raw Message" {
		t.Errorf("expected Title='Raw Message' (fallback due to string id), got %s", msg.Title)
	}
	case err := <-consumer.Errors():
		// 消费者组初始化错误可接受（可能已存在），继续等待消息
		t.Logf("received error (may be expected): %v", err)
		select {
		case msg := <-consumer.Messages():
			t.Logf("received stream message after error: level=%s title=%s", msg.Level, msg.Title)
			if msg.Source != "integration-test" {
				t.Errorf("expected Source=integration-test (partially unmarshaled), got %s", msg.Source)
			}
			if msg.Title != "Raw Message" {
				t.Errorf("expected Title='Raw Message' (fallback), got %s", msg.Title)
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for stream message after error")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for stream message")
	}

	cleanupRedisKeys(t)
}

// TestIntegrationMultipleLevels 集成测试：发送多条不同级别的报警消息
func TestIntegrationMultipleLevels(t *testing.T) {
	skipIfNoRedis(t)

	cfg := &config.RedisConfig{
		Enabled: true,
		Addr:    redisIntegrationAddr,
		Channel: fmt.Sprintf("%s:multi", redisIntegrationChannel),
		Mode:    "pubsub",
	}

	consumer, err := NewRedisConsumer(cfg)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}
	defer consumer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := consumer.Start(ctx); err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	publisher := redis.NewClient(&redis.Options{Addr: redisIntegrationAddr})
	defer publisher.Close()

	// 等待消费者订阅就绪
	time.Sleep(500 * time.Millisecond)

	levels := []string{"info", "warn", "error"}
	for i, level := range levels {
		testMsg := map[string]interface{}{
			"id":      int64(8000 + i),
			"source":  "integration-test",
			"level":   level,
			"title":   fmt.Sprintf("Test Alert [%s]", level),
			"content": fmt.Sprintf("This is a %s level test message", level),
		}
		data, _ := json.Marshal(testMsg)
		_, err := publisher.Publish(ctx, cfg.Channel, string(data)).Result()
		if err != nil {
			t.Fatalf("failed to publish %s message: %v", level, err)
		}
	}

	// 接收并验证 3 条消息
	receivedLevels := []string{}
	for {
		select {
		case msg := <-consumer.Messages():
			receivedLevels = append(receivedLevels, msg.Level)
			t.Logf("received: level=%s title=%s", msg.Level, msg.Title)
			if len(receivedLevels) == 3 {
				goto done
			}
		case err := <-consumer.Errors():
			t.Fatalf("received error: %v", err)
		case <-ctx.Done():
			t.Fatalf("timeout: only received %d of 3 messages", len(receivedLevels))
		}
	}

done:
	if len(receivedLevels) != 3 {
		t.Errorf("expected 3 messages, got %d", len(receivedLevels))
	}
	for i, level := range levels {
		if i < len(receivedLevels) && receivedLevels[i] != level {
			t.Errorf("message %d: expected level=%s, got %s", i, level, receivedLevels[i])
		}
	}
}
