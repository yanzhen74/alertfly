package consumer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
)

// newTestRedisConsumer 创建用于测试的 RedisConsumer（不连接真实 Redis）
func newTestRedisConsumer(mode string) *RedisConsumer {
	return &RedisConsumer{
		cfg: &config.RedisConfig{
			Enabled:       true,
			Addr:          "localhost:6379",
			Channel:       "alertfly:test",
			Stream:        "alertfly:test_stream",
			ConsumerGroup: "alertfly-test-group",
			Mode:          mode,
		},
		msgCh: make(chan *model.Message, 256),
		errCh: make(chan error, 16),
	}
}

// ---------- TestParseMessage ----------

func TestParseMessage(t *testing.T) {
	c := newTestRedisConsumer("pubsub")

	now := time.Now().Truncate(time.Millisecond)

	// 注意：parseMessage 先设置 Source="redis" 和 Topic=topic，
	// 然后 json.Unmarshal 会用 JSON 字段覆盖这些默认值。
	// 此处测试 JSON 中包含 source/topic 字段的情况，验证 JSON 字段覆盖默认值的行为。
	payload := map[string]interface{}{
		"id":      int64(1001),
		"source":  "monitor",
		"topic":   "alerts",
		"level":   "error",
		"subtype": "cpu_alert",
		"sender":  "prometheus",
		"mission": "infra-monitor",
		"title":   "CPU usage over 90%",
		"content": "CPU usage has exceeded 90% for 5 minutes",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal test payload: %v", err)
	}

	msg := c.parseMessage(string(data), "alertfly:test")

	// JSON 中的 source/topic 会覆盖默认值
	if msg.Source != "monitor" {
		t.Errorf("expected Source=monitor (from JSON), got %s", msg.Source)
	}
	if msg.Topic != "alerts" {
		t.Errorf("expected Topic=alerts (from JSON), got %s", msg.Topic)
	}
	if msg.ID != 1001 {
		t.Errorf("expected ID=1001, got %d", msg.ID)
	}
	if msg.Level != "error" {
		t.Errorf("expected Level=error, got %s", msg.Level)
	}
	if msg.SubType != "cpu_alert" {
		t.Errorf("expected SubType=cpu_alert, got %s", msg.SubType)
	}
	if msg.Sender != "prometheus" {
		t.Errorf("expected Sender=prometheus, got %s", msg.Sender)
	}
	if msg.Mission != "infra-monitor" {
		t.Errorf("expected Mission=infra-monitor, got %s", msg.Mission)
	}
	if msg.Title != "CPU usage over 90%" {
		t.Errorf("expected Title='CPU usage over 90%%', got %s", msg.Title)
	}
	if msg.Content != "CPU usage has exceeded 90% for 5 minutes" {
		t.Errorf("unexpected Content: %s", msg.Content)
	}

	// 验证时间戳被设置（允许 1 秒误差）
	if msg.ReceivedAt.Sub(now) > time.Second {
		t.Errorf("ReceivedAt seems wrong: %v", msg.ReceivedAt)
	}
}

func TestParseMessageWithoutSourceTopic(t *testing.T) {
	c := newTestRedisConsumer("pubsub")

	// 测试 JSON 中不包含 source/topic 字段时，默认值保持不变
	payload := map[string]interface{}{
		"id":      int64(1002),
		"level":   "warn",
		"title":   "Disk usage high",
		"content": "Disk /data is 85%% full",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal test payload: %v", err)
	}

	msg := c.parseMessage(string(data), "alertfly:test")

	if msg.Source != "redis" {
		t.Errorf("expected Source=redis (default), got %s", msg.Source)
	}
	if msg.Topic != "alertfly:test" {
		t.Errorf("expected Topic=alertfly:test (default), got %s", msg.Topic)
	}
	if msg.Level != "warn" {
		t.Errorf("expected Level=warn, got %s", msg.Level)
	}
	if msg.Title != "Disk usage high" {
		t.Errorf("expected Title='Disk usage high', got %s", msg.Title)
	}
}

// ---------- TestParseMessageRaw ----------

func TestParseMessageRaw(t *testing.T) {
	c := newTestRedisConsumer("pubsub")

	// 发送一段非 JSON 的纯文本
	rawPayload := "This is a plain text alert message"
	msg := c.parseMessage(rawPayload, "alertfly:test")

	if msg.Source != "redis" {
		t.Errorf("expected Source=redis, got %s", msg.Source)
	}
	if msg.Topic != "alertfly:test" {
		t.Errorf("expected Topic=alertfly:test, got %s", msg.Topic)
	}
	if msg.Title != "Raw Message" {
		t.Errorf("expected Title='Raw Message', got %s", msg.Title)
	}
	if msg.Content != rawPayload {
		t.Errorf("expected Content='%s', got '%s'", rawPayload, msg.Content)
	}
}

func TestParseMessageEmpty(t *testing.T) {
	c := newTestRedisConsumer("pubsub")

	msg := c.parseMessage("", "alertfly:test")

	if msg.Source != "redis" {
		t.Errorf("expected Source=redis, got %s", msg.Source)
	}
	// 空字符串不是有效 JSON，应走 fallback
	if msg.Title != "Raw Message" {
		t.Errorf("expected Title='Raw Message', got %s", msg.Title)
	}
	if msg.Content != "" {
		t.Errorf("expected empty Content, got '%s'", msg.Content)
	}
}

func TestParseMessageInvalidJSON(t *testing.T) {
	c := newTestRedisConsumer("pubsub")

	msg := c.parseMessage("{invalid json", "alertfly:test")

	if msg.Title != "Raw Message" {
		t.Errorf("expected Title='Raw Message', got %s", msg.Title)
	}
	if msg.Content != "{invalid json" {
		t.Errorf("expected Content='{invalid json', got '%s'", msg.Content)
	}
}

// ---------- TestParseStreamMessage ----------

// TestParseStreamMessage 测试 Stream 消息解析
// 注意：Redis Stream 的 XMessage.Values 中所有值都是 string 类型，
// 当 id 字段为字符串时，json.Unmarshal 无法将其解析到 int64 类型的 ID 字段，
// 因此会走 fallback 分支（Title="Raw Message"）。
func TestParseStreamMessage(t *testing.T) {
	c := newTestRedisConsumer("stream")

	now := time.Now().Truncate(time.Millisecond)
	xmsg := redis.XMessage{
		ID: "1234567890-0",
		Values: map[string]interface{}{
			"id":      "2001",
			"source":  "grafana",
			"topic":   "metrics",
			"level":   "warn",
			"subtype": "disk_alert",
			"sender":  "node-exporter",
			"mission": "disk-monitor",
			"title":   "Disk usage over 80%",
			"content": "/data partition is 85% full",
		},
	}

	msg := c.parseStreamMessage(xmsg, "alertfly:test_stream")

	// json.Unmarshal 失败时会部分修改 msg 字段（Source/Topic 被 JSON 值覆盖），
	// 然后 Title 和 Content 被 fallback 分支重写
	if msg.Source != "grafana" {
		t.Errorf("expected Source=grafana (partially unmarshaled), got %s", msg.Source)
	}
	if msg.Topic != "metrics" {
		t.Errorf("expected Topic=metrics (partially unmarshaled), got %s", msg.Topic)
	}
	if msg.Title != "Raw Message" {
		t.Errorf("expected Title='Raw Message' (fallback), got %s", msg.Title)
	}
	// fallback 分支将整个 JSON 字符串放入 Content
	if msg.Content == "" {
		t.Error("expected non-empty Content in fallback")
	}
	if !strings.Contains(msg.Content, "Disk usage over 80%") {
		t.Errorf("expected Content to contain 'Disk usage over 80%%', got %s", msg.Content)
	}
	if msg.ReceivedAt.Sub(now) > time.Second {
		t.Errorf("ReceivedAt seems wrong: %v", msg.ReceivedAt)
	}
}

// TestParseStreamMessageNoID 测试不含 id 字段的 Stream 消息可以正常解析
func TestParseStreamMessageNoID(t *testing.T) {
	c := newTestRedisConsumer("stream")

	xmsg := redis.XMessage{
		ID: "1234567891-0",
		Values: map[string]interface{}{
			"level":   "info",
			"title":   "Service started",
			"content": "AlertFly service has been started successfully",
		},
	}

	msg := c.parseStreamMessage(xmsg, "alertfly:test_stream")

	if msg.Source != "redis" {
		t.Errorf("expected Source=redis, got %s", msg.Source)
	}
	if msg.Topic != "alertfly:test_stream" {
		t.Errorf("expected Topic=alertfly:test_stream, got %s", msg.Topic)
	}
	if msg.Level != "info" {
		t.Errorf("expected Level=info, got %s", msg.Level)
	}
	if msg.Title != "Service started" {
		t.Errorf("expected Title='Service started', got %s", msg.Title)
	}
	if msg.Content != "AlertFly service has been started successfully" {
		t.Errorf("unexpected Content: %s", msg.Content)
	}
}

func TestParseStreamMessageEmpty(t *testing.T) {
	c := newTestRedisConsumer("stream")

	xmsg := redis.XMessage{
		ID:     "999-0",
		Values: map[string]interface{}{},
	}

	msg := c.parseStreamMessage(xmsg, "alertfly:test_stream")

	if msg.Source != "redis" {
		t.Errorf("expected Source=redis, got %s", msg.Source)
	}
	if msg.Topic != "alertfly:test_stream" {
		t.Errorf("expected Topic=alertfly:test_stream, got %s", msg.Topic)
	}
}

// ---------- TestNewRedisConsumer ----------

func TestNewRedisConsumer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.RedisConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
			errMsg:  "redis config is nil",
		},
		{
			name: "invalid mode",
			cfg: &config.RedisConfig{
				Addr: "localhost:6379",
				Mode: "invalid",
			},
			wantErr: true,
			errMsg:  "unsupported redis mode",
		},
		{
			name: "valid pubsub mode",
			cfg: &config.RedisConfig{
				Addr:    "localhost:6379",
				Channel: "alerts",
				Mode:    "pubsub",
			},
			wantErr: false,
		},
		{
			name: "valid stream mode",
			cfg: &config.RedisConfig{
				Addr:          "localhost:6379",
				Stream:        "alert_stream",
				ConsumerGroup: "alertfly-group",
				Mode:          "stream",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer, err := NewRedisConsumer(tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errMsg)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errMsg, err.Error())
				}
				if consumer != nil {
					t.Errorf("expected nil consumer on error, got non-nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if consumer == nil {
					t.Error("expected non-nil consumer, got nil")
				}
				// 清理：关闭消费者
				if consumer != nil {
					consumer.Close()
				}
			}
		})
	}
}

// ---------- TestRedisConsumerChannels ----------

func TestRedisConsumerChannels(t *testing.T) {
	cfg := &config.RedisConfig{
		Addr:    "localhost:6379",
		Channel: "alerts",
		Mode:    "pubsub",
	}

	consumer, err := NewRedisConsumer(cfg)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}
	defer consumer.Close()

	// 验证 Messages() 返回的通道
	msgCh := consumer.Messages()
	if msgCh == nil {
		t.Error("Messages() returned nil channel")
	}

	// msgCh 类型为 <-chan *model.Message，验证不为 nil 即可
	_ = msgCh // msgCh 类型为 <-chan *model.Message，正确

	// 验证 Errors() 返回的通道
	errCh := consumer.Errors()
	if errCh == nil {
		t.Error("Errors() returned nil channel")
	}
	_ = errCh // errCh 类型为 <-chan error，正确
}

func TestRedisConsumerClose(t *testing.T) {
	cfg := &config.RedisConfig{
		Addr:    "localhost:6379",
		Channel: "alerts",
		Mode:    "pubsub",
	}

	consumer, err := NewRedisConsumer(cfg)
	if err != nil {
		t.Fatalf("failed to create consumer: %v", err)
	}

	// 关闭一次
	err = consumer.Close()
	if err != nil {
		t.Errorf("first Close() returned error: %v", err)
	}

	// 重复关闭不应报错
	err = consumer.Close()
	if err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}

	// 关闭后通道应已关闭
	_, ok := <-consumer.Messages()
	if ok {
		t.Error("expected Messages() channel to be closed after Close()")
	}

	_, ok = <-consumer.Errors()
	if ok {
		t.Error("expected Errors() channel to be closed after Close()")
	}
}

