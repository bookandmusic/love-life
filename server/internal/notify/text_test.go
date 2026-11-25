package notify

import (
	"strings"
	"testing"
)

// 互动文案的两个视角：收件人视角用「你的」，群 / 共享渠道必须具名——群里没有
// 收件人，写「你的」会让所有人以为在说自己。
func TestInteractionViews(t *testing.T) {
	cases := []struct {
		name         string
		got          Texts
		wantPersonal string
		wantGroup    string
	}{
		{
			name:         "评论对方动态",
			got:          Commented("小六", "小五", "真好看", false),
			wantPersonal: "小六 评论了你的动态：真好看",
			wantGroup:    "小六 评论了小五的动态：真好看",
		},
		{
			name:         "赞对方动态",
			got:          Liked("小六", "小五", false),
			wantPersonal: "小六 赞了你的动态",
			wantGroup:    "小六 赞了小五的动态",
		},
		{
			name:         "自评自己的动态",
			got:          Commented("小六", "小六", "补一句", true),
			wantPersonal: "小六 评论了自己的动态：补一句",
			wantGroup:    "小六 评论了自己的动态：补一句",
		},
		{
			name:         "自赞自己的动态",
			got:          Liked("小六", "小六", true),
			wantPersonal: "小六 赞了自己的动态",
			wantGroup:    "小六 赞了自己的动态",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got.Personal != c.wantPersonal {
				t.Errorf("Personal = %q，want %q", c.got.Personal, c.wantPersonal)
			}
			if c.got.Group != c.wantGroup {
				t.Errorf("Group = %q，want %q", c.got.Group, c.wantGroup)
			}
		})
	}
}

// 作者名缺失时群视角回退「对方」，不能拼出「的动态」这种残句。
func TestGroupViewFallsBackOnMissingAuthor(t *testing.T) {
	if got := Liked("小六", "", false).Group; got != "小六 赞了对方的动态" {
		t.Fatalf("Group = %q", got)
	}
}

// 句中不含人称的事件两视角同文。
func TestSameTextEvents(t *testing.T) {
	for name, got := range map[string]Texts{
		"新动态": Posted("小六", "今天很好"),
		"相册":  AlbumCreated("小六", "海边"),
		"足迹":  FootprintAdded("小六", "家"),
	} {
		if got.Personal != got.Group {
			t.Errorf("%s 两视角应同文：Personal = %q，Group = %q", name, got.Personal, got.Group)
		}
	}
	if got := Posted("小六", "今天很好").Personal; got != "小六 发布了新动态：今天很好" {
		t.Errorf("新动态 = %q", got)
	}
	if got := FootprintAdded("小六", "家").Personal; got != "小六 添加了足迹「家」" {
		t.Errorf("足迹 = %q", got)
	}
	if got := AnniversarySoon("在一起", 3, "2026-11-28").Personal; got != "纪念日「在一起」还有 3 天（2026-11-28）" {
		t.Errorf("纪念日 = %q", got)
	}
}

// 纯媒体 / 纯地点动态没有正文：句子不得以冒号结尾（推送里挂个空冒号是坏的）。
func TestPostedWithoutContentHasNoTrailingColon(t *testing.T) {
	for _, got := range []string{Posted("小六", "").Personal, Posted("小六", "").Group} {
		if got != "小六 发布了新动态" {
			t.Errorf("无正文的新动态 = %q，want %q", got, "小六 发布了新动态")
		}
	}
}

// 评论正文按 50 字截断并带省略号：超长评论不应把整条推送撑爆。
func TestCommentedTruncatesContent(t *testing.T) {
	got := Commented("小六", "小五", strings.Repeat("啊", 60), false).Personal
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("超长评论应带省略号：%q", got)
	}
	if n := len([]rune(got)); n != len([]rune("小六 评论了你的动态："))+50+1 {
		t.Fatalf("截断长度 = %d，want %d", n, len([]rune("小六 评论了你的动态："))+50+1)
	}
}

// 删除文案带内容名，无名字的类型不留空括号。
func TestDeletedText(t *testing.T) {
	if got := Deleted("小六", "动态", ""); got != "小六 删除了你的动态" {
		t.Errorf("动态 = %q", got)
	}
	if got := Deleted("小六", "相册", "海边"); got != "小六 删除了你的相册「海边」" {
		t.Errorf("相册 = %q", got)
	}
}

// 新动态正文按 100 字截断：长文不该把整条推送撑爆。
func TestPostedTruncatesContent(t *testing.T) {
	got := Posted("小六", strings.Repeat("啊", 120)).Personal
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("超长正文应带省略号：%q", got)
	}
	if n, want := len([]rune(got)), len([]rune("小六 发布了新动态："))+100+1; n != want {
		t.Fatalf("截断长度 = %d，want %d", n, want)
	}
}
