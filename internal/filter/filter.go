// Package filter 提供 AlertFly 消息接收过滤能力。
//
// 规则语法（对每个维度独立生效）：
//   - "xxx"          精确匹配（大小写不敏感）；关键字类维度为子串匹配
//   - "regex:PATTERN" 正则匹配（大小写不敏感）
//   - "!xxx"         排除规则，精确/子串匹配
//   - "!regex:PATTERN" 排除规则，正则匹配
//
// 组合语义（排除优先）：
//   - 空规则集：全部通过（不过滤该维度）
//   - 命中任一排除规则：拒绝
//   - 存在包含规则时：至少命中一条包含规则才通过，否则拒绝
//   - 仅有排除规则且都未命中：通过
package filter

import (
	"regexp"
	"strings"

	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/logger"
	"github.com/oliverxu/alertfly/internal/model"
)

const (
	prefixExclude      = "!"
	prefixRegex        = "regex:"
	prefixExcludeRegex = "!regex:"
)

// Rule 单条已解析的过滤规则
type Rule struct {
	Exclude  bool           // true=排除规则
	IsRegex  bool           // true=正则匹配
	Pattern  string         // 去除前缀后的原始模式
	lower    string         // 非正则时的小写形式，用于大小写不敏感匹配
	compiled *regexp.Regexp // 正则模式（已加 (?i)）
}

// RuleSet 一组同维度的过滤规则
type RuleSet struct {
	rules         []Rule
	hasInclude    bool
	substringMode bool // true=非正则规则采用子串匹配（用于 Title/Content 关键字）
}

// Matcher 过滤匹配器。构建后不可变，可安全并发使用。
type Matcher struct {
	missions        RuleSet
	senders         RuleSet
	subTypes        RuleSet
	levels          RuleSet
	titleKeywords   RuleSet
	contentKeywords RuleSet
}

// NewMatcher 根据过滤配置构建匹配器；正则会在此处一次性编译。
// 无效的正则会被忽略并记录一条 WARN 日志，避免影响其它规则。
func NewMatcher(cfg *config.FilterConfig) *Matcher {
	if cfg == nil {
		cfg = &config.FilterConfig{}
	}
	return &Matcher{
		missions:        newRuleSet(cfg.Missions, false),
		senders:         newRuleSet(cfg.Senders, false),
		subTypes:        newRuleSet(cfg.SubTypes, false),
		levels:          newRuleSet(cfg.Levels, false),
		titleKeywords:   newRuleSet(cfg.TitleKeywords, true),
		contentKeywords: newRuleSet(cfg.ContentKeywords, true),
	}
}

// ShouldNotify 判断消息是否应弹窗通知。
// Source == "system" 的消息始终通过（如版本更新事件）。
func (m *Matcher) ShouldNotify(msg *model.Message) bool {
	if msg == nil {
		return false
	}
	if strings.EqualFold(msg.Source, "system") {
		return true
	}
	if !m.missions.Match(msg.Mission) {
		return false
	}
	if !m.senders.Match(msg.Sender) {
		return false
	}
	if !m.subTypes.Match(msg.SubType) {
		return false
	}
	if !m.levels.Match(msg.Level) {
		return false
	}
	if !m.titleKeywords.Match(msg.Title) {
		return false
	}
	if !m.contentKeywords.Match(msg.Content) {
		return false
	}
	return true
}

// Match 判断单个值是否通过该维度的规则集（排除优先）。
func (rs RuleSet) Match(value string) bool {
	if len(rs.rules) == 0 {
		return true
	}
	lowerValue := strings.ToLower(value)

	// 1) 排除优先：命中任一排除规则即拒绝
	for i := range rs.rules {
		r := &rs.rules[i]
		if !r.Exclude {
			continue
		}
		if ruleHit(r, value, lowerValue, rs.substringMode) {
			return false
		}
	}

	// 2) 仅有排除规则且都未命中：通过
	if !rs.hasInclude {
		return true
	}

	// 3) 有包含规则：至少命中一条才通过
	for i := range rs.rules {
		r := &rs.rules[i]
		if r.Exclude {
			continue
		}
		if ruleHit(r, value, lowerValue, rs.substringMode) {
			return true
		}
	}
	return false
}

// HasRules 报告该维度是否配置了任何有效规则。
func (rs RuleSet) HasRules() bool {
	return len(rs.rules) > 0
}

func ruleHit(r *Rule, value, lowerValue string, substringMode bool) bool {
	if r.IsRegex {
		if r.compiled == nil {
			return false
		}
		return r.compiled.MatchString(value)
	}
	if substringMode {
		return r.lower != "" && strings.Contains(lowerValue, r.lower)
	}
	return lowerValue == r.lower
}

func newRuleSet(items []string, substringMode bool) RuleSet {
	rs := RuleSet{substringMode: substringMode}
	for _, item := range items {
		r, ok := parseRule(item)
		if !ok {
			continue
		}
		rs.rules = append(rs.rules, r)
		if !r.Exclude {
			rs.hasInclude = true
		}
	}
	return rs
}

// parseRule 解析单条规则字符串。空串或无效正则返回 ok=false。
func parseRule(raw string) (Rule, bool) {
	var r Rule
	s := strings.TrimSpace(raw)
	if s == "" {
		return r, false
	}

	switch {
	case strings.HasPrefix(s, prefixExcludeRegex):
		r.Exclude = true
		r.IsRegex = true
		s = strings.TrimPrefix(s, prefixExcludeRegex)
	case strings.HasPrefix(s, prefixRegex):
		r.IsRegex = true
		s = strings.TrimPrefix(s, prefixRegex)
	case strings.HasPrefix(s, prefixExclude):
		r.Exclude = true
		s = strings.TrimPrefix(s, prefixExclude)
	}

	s = strings.TrimSpace(s)
	if s == "" {
		return r, false
	}
	r.Pattern = s

	if r.IsRegex {
		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			logger.Warn("[filter] 无效正则 %q，已忽略该规则: %v", s, err)
			return r, false
		}
		r.compiled = re
	} else {
		r.lower = strings.ToLower(s)
	}
	return r, true
}
