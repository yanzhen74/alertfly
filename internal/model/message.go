package model

import "time"

// Message 统一消息结构
type Message struct {
	ID         int64     `json:"id"`
	Source     string    `json:"source"`     // redis / kafka
	Topic      string    `json:"topic"`      // topic 或 channel 名称
	Level      string    `json:"level"`      // info / warn / error
	SubType    string    `json:"subtype"`    // 消息子类型，过滤字段
	Title      string    `json:"title"`
	Mission    string    `json:"mission"`    // 任务名称，过滤字段
	Sender     string    `json:"sender"`     // 发送者，过滤字段
	Content    string    `json:"content"`    // 自定义 JSON 格式内容
	ReceivedAt time.Time `json:"received_at"`
}