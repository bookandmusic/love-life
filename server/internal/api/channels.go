package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

// 统一选路接口（GET /api/v1/channels）。
//
// 为什么要有它：绿联走 /uglink/channel、备用走 /channels/backup、内置写在 BuildConfig，
// APP 得拼三条逻辑才知道往哪发，而三条路径各自没有可比的优先级。
// 这个接口把「后端已知的全部通道」加上权重一次给出，APP 只做一件事：
// **按权重降序逐个探活，取第一个通的**。
//
// 内置地址不在这里：它由构建期注入、权重恒 100（channelWeightBuiltin），
// APP 侧自己拼在最前——后端改不了它，避免「后端配错一个值把 APP 锁死在不可达地址」。

// channelWeightBuiltin 内置地址权重：恒为最高，后端不可配。
const channelWeightBuiltin = 100

// channelEntry 一条候选通道。
type channelEntry struct {
	Kind string `json:"kind"` // uglink | backup
	// URL 绝对地址（已去尾斜杠）
	URL string `json:"url"`
	// Name 展示名（后台配的名字；绿联为「绿联中转」）
	Name string `json:"name"`
	// Weight 排序权重：越大越优先
	Weight int `json:"weight"`
	// Cookie 仅绿联通道携带（中转自己的会话凭据）；其余为空
	Cookie string `json:"cookie,omitempty"`
	// ExpiresAt 仅绿联通道携带（RFC3339）：到期前 APP 会来重新取一次
	ExpiresAt string `json:"expires_at,omitempty"`
}

// channels 下发全部后端侧通道，按权重降序。
//
// 绿联拿不到会话时**不报错也不出现**（它的 cookie 是必需凭据，
// 没 cookie 的绿联地址对 APP 毫无用处）；备用地址按启用过滤。
// 全空是正常状态：APP 回落内置地址。
func (a *uglinkAPI) channels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := make([]channelEntry, 0, 4)

	if e, ok := a.uglinkEntry(ctx); ok {
		out = append(out, e)
	}
	out = append(out, a.backupEntries(ctx)...)

	sort.SliceStable(out, func(i, j int) bool { return out[i].Weight > out[j].Weight })
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out,
		// 内置地址的权重一并下发：APP 不硬编码，换构建口径时只改一处
		"builtin_weight": channelWeightBuiltin,
	})
}

// uglinkEntry 组装绿联通道；未配置 / 未启用 / 拿不到会话 → false。
func (a *uglinkAPI) uglinkEntry(ctx context.Context) (channelEntry, bool) {
	stored, _, err := a.store.LoadForDisplay(ctx)
	if err != nil || stored == nil || !stored.Enabled {
		return channelEntry{}, false
	}
	session, err := a.manager.Session(ctx)
	if err != nil {
		return channelEntry{}, false
	}
	name := strings.TrimSpace(stored.UglinkID)
	if name == "" {
		name = "绿联中转"
	}
	return channelEntry{
		Kind:      "uglink",
		URL:       session.BaseURL,
		Name:      name,
		Weight:    normalizeWeight(stored.Weight),
		Cookie:    session.Cookie,
		ExpiresAt: session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	}, true
}

// backupEntries 组装启用中的备用地址。
func (a *uglinkAPI) backupEntries(ctx context.Context) []channelEntry {
	list, err := a.backup.load(ctx)
	if err != nil {
		return nil
	}
	out := make([]channelEntry, 0, len(list))
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = item.URL
		}
		out = append(out, channelEntry{
			Kind:   "backup",
			URL:    item.URL,
			Name:   name,
			Weight: normalizeWeight(item.Weight),
		})
	}
	return out
}

// checkWeightRange 只校验范围与保留值（不查占用）。
func checkWeightRange(weight int) string {
	if weight < minChannelWeight || weight > maxChannelWeight {
		return "权重需在 1–999 之间"
	}
	if weight == reservedChannelWeight {
		return "100 是内置地址的权重，请换一个（1–99 或 101–999）"
	}
	return ""
}
