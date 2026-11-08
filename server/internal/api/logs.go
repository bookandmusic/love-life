package api

import (
	"net/http"
	"strconv"

	"github.com/bookandmusic/love-life/server/internal/logbuf"
)

// logsAPI 服务日志查询（仅 admin）。
//
// 数据源是进程内环形缓冲，不是数据库：日志本就进 stdout，这里只是让后台能直接看，
// 免去 `make logs` 或翻终端。**重启即丢**是有意取舍——落盘会与「单一数据目录只存业务数据」
// 冲突，而重启前的日志仍在 stdout。
type logsAPI struct {
	buf *logbuf.Buffer
}

// list GET /api/v1/admin/logs?since=<seq>
//
// 契约：{items: [{seq, time, level, msg, attrs}], latestSeq}。
// since 为上次拿到的最大 seq，前端据此**增量**拉取（不重复、不丢）；缺省 0 = 全量。
// attrs 已由缓冲写入侧脱敏（api_key / token / password 等替换为 ••••）。
func (a *logsAPI) list(w http.ResponseWriter, r *http.Request) {
	var since int64
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "since 需为非负整数")
			return
		}
		since = n
	}
	// 服务端重启后缓冲从 seq=1 重新开始，而客户端游标还停在重启前的较大值。
	// 此时 `Since(cursor)` 会永远返回空集——页面看着「在跟随」却再也不出新日志。
	// 游标比当前最大序号还大 = 服务端已换了一轮，当全量拉。
	if latest := a.buf.LatestSeq(); since > latest {
		since = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     a.buf.Since(since),
		"latestSeq": a.buf.LatestSeq(),
	})
}
