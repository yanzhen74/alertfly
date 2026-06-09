package web

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
	"github.com/oliverxu/alertfly/internal/storage"
	"gopkg.in/yaml.v3"
)

// handleMessages GET /api/messages — 查询消息（Layui 表格数据格式）
func (s *WebServer) handleMessages(c *gin.Context) {
	filter := storage.QueryFilter{}

	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	filter.Page = page
	filter.PageSize = pageSize

	// 过滤参数
	filter.Level = c.Query("level")
	filter.Source = c.Query("source")
	filter.Keyword = c.Query("keyword")
	filter.SubType = c.Query("subtype")
	filter.Mission = c.Query("mission")
	filter.Sender = c.Query("sender")

	// 时间参数
	if startTimeStr := c.Query("startTime"); startTimeStr != "" {
		if t, err := parseTimeFlex(startTimeStr); err == nil {
			filter.StartTime = &t
		}
	}
	if endTimeStr := c.Query("endTime"); endTimeStr != "" {
		if t, err := parseTimeFlex(endTimeStr); err == nil {
			filter.EndTime = &t
		}
	}

	// 查询数据
	messages, total, err := s.storage.Query(filter)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 1,
			"msg":  "查询失败: " + err.Error(),
		})
		return
	}

	// 确保返回空数组而非 null
	if messages == nil {
		messages = make([]*model.Message, 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  0,
		"msg":   "success",
		"count": total,
		"data":  messages,
	})
}

// handleGetConfig GET /api/config — 获取当前配置
func (s *WebServer) handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": s.config,
	})
}

// handleUpdateConfig PUT /api/config — 更新配置并写入 YAML 文件
func (s *WebServer) handleUpdateConfig(c *gin.Context) {
	var cfg config.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 1,
			"msg":  "配置解析失败: " + err.Error(),
		})
		return
	}

	// 序列化为 YAML
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 1,
			"msg":  "配置序列化失败: " + err.Error(),
		})
		return
	}

	// 写入配置文件
	if err := os.WriteFile(s.configPath, data, 0644); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 1,
			"msg":  "配置文件写入失败: " + err.Error(),
		})
		return
	}

	// 更新内存中的配置
	*s.config = cfg

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "配置已保存，重启后生效",
	})
}

// handleStatus GET /api/status — 获取连接状态（暂时返回固定值）
func (s *WebServer) handleStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"redis_enabled": s.config.Redis.Enabled,
			"kafka_enabled": s.config.Kafka.Enabled,
			"last_error":    "",
		},
	})
}

// parseTimeFlex 灵活解析时间字符串，支持 "2006-01-02 15:04:05" 和 "2006-01-02" 两种格式
func parseTimeFlex(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02", s, time.Local)
}
