package filter

import (
	"testing"

	"github.com/oliverxu/alertfly/internal/config"
	"github.com/oliverxu/alertfly/internal/model"
)

func TestRuleSet_EmptyPassesAll(t *testing.T) {
	rs := newRuleSet(nil, false)
	if !rs.Match("anything") {
		t.Errorf("空规则集应通过任意值")
	}
	if !rs.Match("") {
		t.Errorf("空规则集应通过空字符串")
	}
}

func TestRuleSet_ExactMatchCaseInsensitive(t *testing.T) {
	rs := newRuleSet([]string{"Deploy", "backup"}, false)
	cases := map[string]bool{
		"deploy": true,
		"DEPLOY": true,
		"backup": true,
		"Backup": true,
		"other":  false,
		"":       false,
	}
	for v, want := range cases {
		if got := rs.Match(v); got != want {
			t.Errorf("Match(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestRuleSet_SubstringMode(t *testing.T) {
	rs := newRuleSet([]string{"失败"}, true)
	if !rs.Match("部署失败：磁盘不足") {
		t.Errorf("子串模式应命中包含关键字的值")
	}
	if rs.Match("部署成功") {
		t.Errorf("子串模式不应命中不含关键字的值")
	}
}

func TestRuleSet_RegexInclude(t *testing.T) {
	rs := newRuleSet([]string{`regex:^deploy-.*`}, false)
	if !rs.Match("deploy-prod") {
		t.Errorf("正则应命中 deploy-prod")
	}
	if !rs.Match("DEPLOY-prod") {
		t.Errorf("正则应大小写不敏感")
	}
	if rs.Match("nightly-deploy-x") {
		t.Errorf("正则不应命中前缀不匹配的值")
	}
}

func TestRuleSet_ExcludeOnly(t *testing.T) {
	// 仅有排除规则：命中即拒绝，未命中则通过
	rs := newRuleSet([]string{"!test", `!regex:^tmp-.*`}, false)
	cases := map[string]bool{
		"test":     false,
		"TEST":     false,
		"tmp-abc":  false,
		"deploy":   true,
		"anything": true,
	}
	for v, want := range cases {
		if got := rs.Match(v); got != want {
			t.Errorf("Match(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestRuleSet_ExcludePriorityOverInclude(t *testing.T) {
	// 排除优先：即使包含规则命中，只要排除规则也命中就拒绝
	rs := newRuleSet([]string{"deploy", "!deploy-test"}, false)
	cases := map[string]bool{
		"deploy":      true,
		"deploy-test": false, // 排除优先
		"other":       false, // 有包含规则但未命中
	}
	for v, want := range cases {
		if got := rs.Match(v); got != want {
			t.Errorf("Match(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestRuleSet_InvalidRegexIgnored(t *testing.T) {
	// 无效正则应被忽略，其它规则照常工作
	rs := newRuleSet([]string{"regex:[invalid", "backup"}, false)
	if !rs.Match("backup") {
		t.Errorf("有效规则应正常命中")
	}
	if rs.Match("other") {
		t.Errorf("无效正则被忽略后，未命中其它规则应拒绝")
	}
}

func TestRuleSet_TrimAndSkipEmpty(t *testing.T) {
	rs := newRuleSet([]string{"  deploy  ", "", "   "}, false)
	if len(rs.rules) != 1 {
		t.Errorf("应仅保留 1 条有效规则，实际 %d", len(rs.rules))
	}
	if !rs.Match("deploy") {
		t.Errorf("前后空白应被裁剪")
	}
}

func TestMatcher_SystemAlwaysPasses(t *testing.T) {
	m := NewMatcher(&config.FilterConfig{
		Levels: []string{"error"},
	})
	msg := &model.Message{Source: "system", Level: "info", Title: "更新"}
	if !m.ShouldNotify(msg) {
		t.Errorf("system 消息应始终通过过滤")
	}
}

func TestMatcher_AllDimensionsCombine(t *testing.T) {
	m := NewMatcher(&config.FilterConfig{
		Missions:        []string{"deploy"},
		Senders:         []string{"ops", "!bot"},
		SubTypes:        []string{},
		Levels:          []string{"warn", "error"},
		TitleKeywords:   []string{"!测试"},
		ContentKeywords: []string{`regex:cpu\s*>\s*\d+`},
	})

	tests := []struct {
		name string
		msg  *model.Message
		want bool
	}{
		{
			name: "全部命中",
			msg: &model.Message{
				Mission: "deploy", Sender: "ops", Level: "error",
				Title: "部署失败", Content: `{"metric":"cpu > 95"}`,
			},
			want: true,
		},
		{
			name: "级别不匹配",
			msg: &model.Message{
				Mission: "deploy", Sender: "ops", Level: "info",
				Title: "部署完成", Content: `cpu > 95`,
			},
			want: false,
		},
		{
			name: "sender 被排除",
			msg: &model.Message{
				Mission: "deploy", Sender: "bot", Level: "error",
				Title: "x", Content: `cpu > 95`,
			},
			want: false,
		},
		{
			name: "标题命中排除关键字",
			msg: &model.Message{
				Mission: "deploy", Sender: "ops", Level: "error",
				Title: "【测试】cpu告警", Content: `cpu > 95`,
			},
			want: false,
		},
		{
			name: "内容正则未命中",
			msg: &model.Message{
				Mission: "deploy", Sender: "ops", Level: "error",
				Title: "告警", Content: `memory low`,
			},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.ShouldNotify(tc.msg); got != tc.want {
				t.Errorf("ShouldNotify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatcher_NilConfigAcceptsAll(t *testing.T) {
	m := NewMatcher(nil)
	if !m.ShouldNotify(&model.Message{Source: "redis", Level: "info"}) {
		t.Errorf("nil 配置应放行所有消息")
	}
}
