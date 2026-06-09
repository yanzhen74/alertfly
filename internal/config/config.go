package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 全局配置结构
type Config struct {
	Redis    RedisConfig    `yaml:"redis"`
	Kafka    KafkaConfig    `yaml:"kafka"`
	Storage  StorageConfig  `yaml:"storage"`
	Notifier NotifierConfig `yaml:"notifier"`
	Updater  UpdaterConfig  `yaml:"updater"`
	Consumer ConsumerConfig `yaml:"consumer"`
}

// RedisConfig Redis 连接配置
type RedisConfig struct {
	Addr          string `yaml:"addr"`           // Redis 地址，如 localhost:6379
	Password      string `yaml:"password"`       // Redis 密码
	DB            int    `yaml:"db"`             // Redis 数据库编号
	Channel       string `yaml:"channel"`        // Pub/Sub channel 名称
	Stream        string `yaml:"stream"`         // Stream 名称
	ConsumerGroup string `yaml:"consumer_group"` // Stream Consumer Group 名称
	Mode          string `yaml:"mode"`           // pubsub 或 stream
}

// KafkaConfig Kafka 连接配置
type KafkaConfig struct {
	Brokers []string `yaml:"brokers"` // Kafka broker 地址列表
	Topic   string   `yaml:"topic"`   // Kafka topic 名称
	GroupID string   `yaml:"group_id"` // Kafka Consumer Group ID
}

// StorageConfig 本地存储配置
type StorageConfig struct {
	DBPath       string `yaml:"db_path"`        // SQLite 数据库文件路径
	RetentionDays int   `yaml:"retention_days"` // 消息保留天数
	MaxRecords   int    `yaml:"max_records"`    // 最大保留记录数
}

// NotifierConfig 通知配置
type NotifierConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用桌面通知
}

// UpdaterConfig 自动更新配置
type UpdaterConfig struct {
	Enabled   bool        `yaml:"enabled"`    // 是否启用自动更新检查
	CheckURL  string      `yaml:"check_url"`  // 检查更新的 URL
	Interval  time.Duration `yaml:"interval"` // 检查间隔（Go duration 格式，如 24h）
}

// ConsumerConfig 消费者类型配置
type ConsumerConfig struct {
	Type string `yaml:"type"` // redis 或 kafka
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

	return cfg, nil
}