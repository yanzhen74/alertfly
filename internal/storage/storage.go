package storage

import (
	"time"

	"github.com/oliverxu/alertfly/internal/model"
)

// QueryFilter 查询过滤条件
type QueryFilter struct {
	StartTime *time.Time
	EndTime   *time.Time
	Level     string
	Keyword   string
	Source    string
	SubType   string
	Mission   string
	Sender    string
	Page      int
	PageSize  int
}

// Storage 消息持久化存储接口
type Storage interface {
	// Save 插入一条消息
	Save(msg *model.Message) error

	// Query 按条件查询消息，返回消息列表和总数
	Query(filter QueryFilter) ([]*model.Message, int64, error)

	// Cleanup 清理过期记录：删除超过 retentionDays 天的记录，
	// 或当总记录数超过 maxRecords 时删除最早的记录
	Cleanup(retentionDays int, maxRecords int) error

	// Close 关闭存储连接
	Close() error
}
