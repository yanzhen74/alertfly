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
	Enabled bool     `yaml:"enabled" json:"enabled"`
	Brokers []string `yaml:"brokers" json:"brokers"`
	Topic   string   `yaml:"topic" json:"topic"`
	GroupID string   `yaml:"group_id" json:"group_id"`
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