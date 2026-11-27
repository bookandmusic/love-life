package notify

import (
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/textutil"
)

// View 通知文案的叙述视角。
type View int

const (
	// ViewPersonal 收件人视角：个人渠道与 APP 站内通知共用，用第二人称（「你的动态」）。
	ViewPersonal View = iota
	// ViewGroup 旁观者视角：群 / 共享渠道，用第三人称具名（「小五的动态」）。
	ViewGroup
)

// Texts 一条通知的两种视角文案。
//
// 通知句子只在本文件生成：同一件事要同时服务点对点（个人渠道 / 站内通知）与广播
// （群渠道），视角不同措辞就不同，调用点各拼一句必然漂移。
type Texts struct {
	Personal string // 个人渠道与 APP 站内通知
	Group    string // 群 / 共享渠道
}

// twoViews 用同一套规则渲染两个视角。
func twoViews(render func(View) string) Texts {
	return Texts{Personal: render(ViewPersonal), Group: render(ViewGroup)}
}

// same 两视角同文的事件（句中不含人称）。
func same(text string) Texts { return Texts{Personal: text, Group: text} }

// owner 互动通知的归属短语：动态作者就是操作者本人 → 「自己的动态」（两个视角一致）；
// 否则收件人视角写「你的动态」，旁观者视角写「<作者>的动态」。
func owner(view View, author string, selfAuthored bool) string {
	if selfAuthored {
		return "自己的动态"
	}
	if view == ViewGroup {
		if author == "" {
			author = "对方"
		}
		return author + "的动态"
	}
	return "你的动态"
}

// Liked 点赞：「小六 赞了你的动态」／「小六 赞了小五的动态」。
func Liked(operator, author string, selfAuthored bool) Texts {
	return twoViews(func(v View) string {
		return operator + " 赞了" + owner(v, author, selfAuthored)
	})
}

// Commented 评论：「小六 评论了你的动态：…」／「小六 评论了小五的动态：…」。
// 纯图片评论没有正文，句末不留空冒号（与 Posted 同口径）。
func Commented(operator, author, content string, selfAuthored bool) Texts {
	text := textutil.TruncMark(content, 50)
	return twoViews(func(v View) string {
		head := operator + " 评论了" + owner(v, author, selfAuthored)
		if text == "" {
			return head
		}
		return head + "：" + text
	})
}

// Posted 新动态：正文即通知内容。纯媒体 / 纯地点动态没有正文，句末不留空冒号。
func Posted(operator, content string) Texts {
	if t := textutil.TruncMark(content, 100); t != "" {
		return same(operator + " 发布了新动态：" + t)
	}
	return same(operator + " 发布了新动态")
}

// AlbumCreated 相册上新。
func AlbumCreated(operator, name string) Texts {
	return same(operator + " 创建了相册「" + name + "」")
}

// FootprintAdded 足迹更新。
func FootprintAdded(operator, name string) Texts {
	return same(operator + " 添加了足迹「" + name + "」")
}

// AnniversarySoon 纪念日临近（定时任务产生，没有操作者）。
func AnniversarySoon(name string, days int, date string) Texts {
	return same("纪念日「" + name + "」还有 " + strconv.Itoa(days) + " 天（" + date + "）")
}

// Deleted 内容被删除：内容删除只发创建者站内通知，没有群渠道事件，故只产出收件人视角一句话。
// label 为内容类型（动态 / 评论 / 相册 / 纪念日 / 足迹）；name 为该类型的名字，无名字时为空。
func Deleted(deleter, label, name string) string {
	object := "你的" + label
	if name != "" {
		object += "「" + name + "」"
	}
	return deleter + " 删除了" + object
}
