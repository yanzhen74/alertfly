package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置结构
type Config struct {
	Redis    RedisConfig    `yaml:"redis" json:"redis"`
	Kafka    KafkaConfig    `yaml:"kafka" json:"kafka"`
	Storage  StorageConfig  `yaml:"storage" json:"storage"`
	Notifier NotifierConfig `yaml:"notifier" json:"notifier"`
	Updater  UpdaterConfig  `yaml:"updater" json:"updater"`
	Web      WebConfig      `yaml:"web" json:"web"`
	Filter   FilterConfig   `yaml:"filter" json:"filter"`
}

// WebConfig Web UI 配置
type WebConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	Port    int  `yaml:"port" json:"port"`
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Addr          string `yaml:"addr" json:"addr"`
	Password      string `yaml:"password" json:"password"`
	DB            int    `yaml:"db" json:"db"`
	Channel       string `yaml:"channel" json:"channel"`
	Stream        string `yaml:"stream" json:"stream"`
	ConsumerGroup string `yaml:"consumer_group" json:"consumer_group"`
	Mode          string `yaml:"mode" json:"mode"`
}

// KafkaConfig Kafka 连接配置
type KafkaConfig struct {
	Enabled           bool     `yaml:"enabled" json:"enabled"`
	Brokers           []string `yaml:"brokers" json:"brokers"`
	Topic             string   `yaml:"topic" json:"topic"`              // 单 topic 配置（向后兼容）
	Topics            []string `yaml:"topics" json:"topics"`            // 多 topic 配置，支持 regex: 前缀正则
	GroupID           string   `yaml:"group_id" json:"group_id"`
	TopicScanInterval int      `yaml:"topic_scan_interval" json:"topic_scan_interval"` // topic 正则扫描间隔（秒），默认 30
	Version           string   `yaml:"version" json:"version"`          // Kafka broker 版本，默认 "2.0.0"
}

// StorageConfig 本地存储配置
type StorageConfig struct {
	DBPath       string `yaml:"db_path" json:"db_path"`
	RetentionDays int   `yaml:"retention_days" json:"retention_days"`
	MaxRecords   int    `yaml:"max_records" json:"max_records"`
}

// NotifierConfig 通知配置
type NotifierConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// UpdaterConfig 自动更新配置
type UpdaterConfig struct {
	Enabled   bool          `yaml:"enabled" json:"enabled"`
	CheckURL  string        `yaml:"check_url" json:"check_url"`
	Interval  time.Duration `yaml:"interval" json:"interval"`
}

// FilterConfig 接收过滤配置，控制哪些消息弹窗通知
// 空列表表示不过滤该维度（接收所有），不匹配的消息仍存储但不弹窗
type FilterConfig struct {
	Missions []string `yaml:"missions" json:"missions"` // 接收的任务名列表，空=全部
	Senders  []string `yaml:"senders" json:"senders"`   // 接收的发送者列表，空=全部
	SubTypes []string `yaml:"subtypes" json:"subtypes"` // 接收的子类型列表，空=全部
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// 向后兼容：如果使用旧的单 topic 配置，自动迁移到 topics 列表
	if cfg.Kafka.Topic != "" && len(cfg.Kafka.Topics) == 0 {
		cfg.Kafka.Topics = []string{cfg.Kafka.Topic}
	}

	return cfg, nil
}