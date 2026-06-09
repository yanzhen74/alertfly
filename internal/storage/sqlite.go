package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/oliverxu/alertfly/internal/model"
)

// SQLiteStorage 基于 SQLite 的消息存储实现
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLiteStorage 创建 SQLite 存储实例，自动建表和创建索引
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// 启用 WAL 模式以提升并发读写性能
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &SQLiteStorage{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return s, nil
}

// initSchema 建表和创建索引
func (s *SQLiteStorage) initSchema() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source TEXT,
		topic TEXT,
		level TEXT,
		subtype TEXT,
		title TEXT,
		mission TEXT,
		sender TEXT,
		content TEXT,
		received_at DATETIME
	);`

	if _, err := s.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_messages_received_at ON messages(received_at)",
		"CREATE INDEX IF NOT EXISTS idx_messages_level ON messages(level)",
		"CREATE INDEX IF NOT EXISTS idx_messages_subtype ON messages(subtype)",
		"CREATE INDEX IF NOT EXISTS idx_messages_mission ON messages(mission)",
		"CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender)",
	}

	for _, idx := range indexes {
		if _, err := s.db.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	return nil
}

// Save 插入一条消息
func (s *SQLiteStorage) Save(msg *model.Message) error {
	_, err := s.db.Exec(
		`INSERT INTO messages (source, topic, level, subtype, title, mission, sender, content, received_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.Source, msg.Topic, msg.Level, msg.SubType, msg.Title,
		msg.Mission, msg.Sender, msg.Content, msg.ReceivedAt,
	)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

// Query 按条件查询消息，返回消息列表和总数
func (s *SQLiteStorage) Query(filter QueryFilter) ([]*model.Message, int64, error) {
	where, args := s.buildWhereClause(filter)

	// 查询总数
	countSQL := "SELECT COUNT(*) FROM messages"
	if where != "" {
		countSQL += " WHERE " + where
	}

	var total int64
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count messages: %w", err)
	}

	// 查询数据列表
	dataSQL := "SELECT id, source, topic, level, subtype, title, mission, sender, content, received_at FROM messages"
	if where != "" {
		dataSQL += " WHERE " + where
	}
	dataSQL += " ORDER BY received_at DESC"

	// 分页
	page := filter.Page
	pageSize := filter.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	dataSQL += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)

	rows, err := s.db.Query(dataSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []*model.Message
	for rows.Next() {
		msg := &model.Message{}
		if err := rows.Scan(
			&msg.ID, &msg.Source, &msg.Topic, &msg.Level, &msg.SubType,
			&msg.Title, &msg.Mission, &msg.Sender, &msg.Content, &msg.ReceivedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows iteration: %w", err)
	}

	return messages, total, nil
}

// buildWhereClause 根据 filter 构建 WHERE 子句和参数
func (s *SQLiteStorage) buildWhereClause(filter QueryFilter) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if filter.StartTime != nil {
		conditions = append(conditions, "received_at >= ?")
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		conditions = append(conditions, "received_at <= ?")
		args = append(args, *filter.EndTime)
	}
	if filter.Level != "" {
		conditions = append(conditions, "level = ?")
		args = append(args, filter.Level)
	}
	if filter.Keyword != "" {
		conditions = append(conditions, "(title LIKE ? OR content LIKE ?)")
		kw := "%" + filter.Keyword + "%"
		args = append(args, kw, kw)
	}
	if filter.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, filter.Source)
	}
	if filter.SubType != "" {
		conditions = append(conditions, "subtype = ?")
		args = append(args, filter.SubType)
	}
	if filter.Mission != "" {
		conditions = append(conditions, "mission = ?")
		args = append(args, filter.Mission)
	}
	if filter.Sender != "" {
		conditions = append(conditions, "sender = ?")
		args = append(args, filter.Sender)
	}

	where := strings.Join(conditions, " AND ")
	return where, args
}

// Cleanup 清理过期记录
func (s *SQLiteStorage) Cleanup(retentionDays int, maxRecords int) error {
	// 1. 删除超过 retentionDays 天的记录
	if retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays)
		if _, err := s.db.Exec("DELETE FROM messages WHERE received_at < ?", cutoff); err != nil {
			return fmt.Errorf("cleanup by retention days: %w", err)
		}
	}

	// 2. 当总记录数超过 maxRecords 时删除最早的记录
	if maxRecords > 0 {
		if _, err := s.db.Exec(
			`DELETE FROM messages WHERE id IN (
				SELECT id FROM messages ORDER BY received_at ASC
				LIMIT (SELECT COUNT(*) FROM messages) - ?
			)`,
			maxRecords,
		); err != nil {
			return fmt.Errorf("cleanup by max records: %w", err)
		}
	}

	return nil
}

// Close 关闭数据库连接
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
