package proxy

import (
	"encoding/json"

	"github.com/oliverxu/alertfly/internal/model"
)

// DefaultJSONAdapter 默认 JSON 适配器，直接将 rawData JSON 反序列化为 model.Message
type DefaultJSONAdapter struct{}

// Name 返回适配器名称
func (a *DefaultJSONAdapter) Name() string {
	return "json"
}

// Parse 将 JSON 格式的原始数据解析为统一 Message 结构
func (a *DefaultJSONAdapter) Parse(rawData []byte) (*model.Message, error) {
	var msg model.Message
	if err := json.Unmarshal(rawData, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
