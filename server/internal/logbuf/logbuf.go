// Package logbuf 捕获进程内日志供后台查看。
//
// 为什么是内存环形缓冲而不是落盘：日志本就进 stdout（容器用 `make logs`、裸跑看终端），
// 这里只是把它同时留一份给「后台服务日志」页。重启即丢属于已知取舍——重启前的日志仍在
// stdout，且再落一份到数据目录会与「单一数据目录只存业务数据」的部署取向冲突。
package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultCapacity 默认保留条数。够看清一次排障上下文，又不会把内存吃出形状。
const DefaultCapacity = 2000

// Entry 一条日志的快照（可直接序列化给前端）。
type Entry struct {
	// Seq 单调递增序号：前端带上次拿到的最大 Seq 轮询，只取增量（不重复、不丢）。
	Seq   int64  `json:"seq"`
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	// Attrs 结构化字段（已脱敏）。保留原始键名，值为字符串化结果。
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Buffer 定长环形缓冲：写满后覆盖最旧的一条。
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	next    int64 // 下一条的 Seq
	cap     int
}

func New(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{entries: make([]Entry, 0, capacity), cap: capacity}
}

// Append 写入一条。Seq 由缓冲自行分配（调用方不用管并发与序号）。
func (b *Buffer) Append(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	e.Seq = b.next
	if len(b.entries) < b.cap {
		b.entries = append(b.entries, e)
		return
	}
	// 满了：丢掉最旧的（copy 前移一位），把新的放在末尾
	copy(b.entries, b.entries[1:])
	b.entries[len(b.entries)-1] = e
}

// Since 返回 Seq 大于 after 的条目（after=0 即全部）。
func (b *Buffer) Since(after int64) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out
}

// LatestSeq 当前最大序号（前端首次拉取后据此增量轮询）。
func (b *Buffer) LatestSeq() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.next
}

// 需要打码的键：值本身是凭据。
//
// 匹配规则刻意**不是**子串包含——`max_tokens` / `prompt_tokens` / `total_tokens` 等是
// token **计数**（排障最需要的信息，仪表盘也把它们当产品指标展示），子串匹配会把它们一并
// 打码，等于把日志页最有用的东西抹掉。故只认「整名命中」与「下划线后缀命中」。
var sensitiveExact = map[string]bool{
	"key": true, "apikey": true, "api_key": true, "token": true, "secret": true,
	"password": true, "passwd": true, "authorization": true, "cookie": true,
	"credential": true, "sig": true, "signature": true,
}

var sensitiveSuffix = []string{
	"_key", "_token", "_secret", "_password", "_passwd", "_credential", "_hash",
}

// Redacted 敏感值的替换文本（导出是为了让测试与调用方引用同一常量，不各写一遍字面量）。
const Redacted = "••••"

// redact 判断某个键的值是否必须打码。
func redact(key string) bool {
	k := strings.ToLower(key)
	if sensitiveExact[k] {
		return true
	}
	// signature_delta（思考签名增量）等以 signature 起头的都要打码
	if strings.HasPrefix(k, "signature") {
		return true
	}
	for _, s := range sensitiveSuffix {
		// 只认后缀：`max_tokens` 以 `_tokens` 结尾，不命中 `_token`
		if strings.HasSuffix(k, s) {
			return true
		}
	}
	return false
}

// Handler 包装下一个 handler：每条记录先留一份进 Buffer，再原样交给下游（stdout 照旧）。
type Handler struct {
	next  slog.Handler
	buf   *Buffer
	attrs []slog.Attr
	group string
}

// NewHandler 用 buf 包住 next（每条记录先留一份进缓冲，再原样交给 next，stdout 输出照旧）。
//
// next 必须是一个**独立**的 handler，不能传 `slog.Default().Handler()` 之后又 SetDefault 成它：
// 那样包的正是 Default 自己，包级 slog.Info 会经由 log.(*Logger).output 自锁（实测直接挂死，
// 既不报错也不监听端口）。故这里对 nil 直接 panic，而不是「贴心地」回落到 Default——
// 那个回落恰好会造出上面那个死锁。
func NewHandler(next slog.Handler, buf *Buffer) *Handler {
	if next == nil {
		panic("logbuf.NewHandler: next 不能为 nil（传一个独立的 handler，参照 main.go 的 downstream）")
	}
	return &Handler{next: next, buf: buf}
}

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := map[string]string{}
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	for _, a := range h.attrs {
		collect(attrs, prefix, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		collect(attrs, prefix, a)
		return true
	})
	if len(attrs) == 0 {
		attrs = nil
	}
	h.buf.Append(Entry{
		Time:  r.Time.Format(time.DateTime),
		Level: r.Level.String(),
		Msg:   r.Message,
		Attrs: attrs,
	})
	return h.next.Handle(ctx, r)
}

// collect 把一个 attr 摊平进 map；组递归展开，敏感键打码。
func collect(dst map[string]string, prefix string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		inner := prefix
		if a.Key != "" {
			inner = prefix + a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			collect(dst, inner, ga)
		}
		return
	}
	if redact(a.Key) {
		dst[prefix+a.Key] = Redacted
		return
	}
	dst[prefix+a.Key] = a.Value.String()
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), buf: h.buf, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), group: h.group}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	// 下游的 group 语义交给 next；这里只累积用于摊平进 Buffer 的键前缀
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &Handler{next: h.next.WithGroup(name), buf: h.buf, attrs: h.attrs, group: g}
}
