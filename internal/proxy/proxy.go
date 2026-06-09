package proxy

import (
	"fmt"

	"github.com/oliverxu/alertfly/internal/model"
)

// Adapter 消息适配器接口，不同系统实现不同的解析逻辑
type Adapter interface {
	// Name 返回适配器名称（用于配置匹配）
	Name() string
	// Parse 将原始消息数据解析为统一 Message 结构
	Parse(rawData []byte) (*model.Message, error)
}

// Proxy 消息代理，管理多个适配器并按来源路由
type Proxy struct {
	adapters    map[string]Adapter // name -> adapter
	topicMap    map[string]string  // topic/channel -> adapter name
	defaultName string             // 默认适配器名称
}

// NewProxy 创建 Proxy 实例
func NewProxy() *Proxy {
	return &Proxy{
		adapters: make(map[string]Adapter),
		topicMap: make(map[string]string),
	}
}

// RegisterAdapter 注册一个适配器
func (p *Proxy) RegisterAdapter(adapter Adapter) {
	p.adapters[adapter.Name()] = adapter
}

// SetTopicAdapter 设置某个 topic/channel 使用指定适配器
func (p *Proxy) SetTopicAdapter(topic string, adapterName string) {
	p.topicMap[topic] = adapterName
}

// SetDefault 设置默认适配器名称
func (p *Proxy) SetDefault(name string) {
	p.defaultName = name
}

// Transform 根据 topic 路由到对应适配器进行消息转换
// 如果 topic 没有指定适配器，使用默认适配器
func (p *Proxy) Transform(topic string, rawData []byte) (*model.Message, error) {
	adapterName, ok := p.topicMap[topic]
	if !ok {
		adapterName = p.defaultName
	}

	adapter, ok := p.adapters[adapterName]
	if !ok {
		return nil, fmt.Errorf("adapter not found: %s", adapterName)
	}

	msg, err := adapter.Parse(rawData)
	if err != nil {
		return nil, fmt.Errorf("adapter %s parse failed: %w", adapterName, err)
	}

	// 如果消息中没有指定 topic，用传入的 topic 补充
	if msg.Topic == "" {
		msg.Topic = topic
	}

	return msg, nil
}
