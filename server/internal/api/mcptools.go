package api

import (
	"encoding/json"
	"net/http"

	"github.com/bookandmusic/love-life/server/internal/store"
	"github.com/bookandmusic/love-life/server/internal/tools"
)

// mcpToolsAPI MCP 工具注册表与启停（AD4，DESIGN §6 / §9）。
type mcpToolsAPI struct {
	db *store.DB
}

// toolItem 契约形状（routes.mjs /admin/mcp-tools）：
// {name, description, category, enabled, schema}。
type toolItem struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Enabled     bool              `json:"enabled"`
	Schema      map[string]string `json:"schema"`
}

func (a *mcpToolsAPI) list(w http.ResponseWriter, r *http.Request) {
	disabled, err := a.db.DisabledTools(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}

	all := tools.Registry()
	out := make([]toolItem, 0, len(all))
	for _, t := range all {
		schema := t.Schema
		if schema == nil {
			schema = map[string]string{}
		}
		out = append(out, toolItem{
			Name:        t.Name,
			Description: t.Description,
			Category:    t.Category,
			Enabled:     !disabled[t.Name],
			Schema:      schema,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// patch PATCH /api/v1/admin/mcp-tools/{name}
// 契约：enabled 必填（boolean）→ 400「enabled 必填（boolean）」；工具不存在 → 404「工具不存在」。
func (a *mcpToolsAPI) patch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	var target *tools.Tool
	for _, t := range tools.Registry() {
		if t.Name == name {
			target = t
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "工具不存在")
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled 必填（boolean）")
		return
	}

	if err := a.db.SetToolEnabled(r.Context(), name, *req.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "服务器错误")
		return
	}

	schema := target.Schema
	if schema == nil {
		schema = map[string]string{}
	}
	writeJSON(w, http.StatusOK, toolItem{
		Name:        target.Name,
		Description: target.Description,
		Category:    target.Category,
		Enabled:     *req.Enabled,
		Schema:      schema,
	})
}
