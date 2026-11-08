// Package geo 离线城市判定：内置地级市行政边界多边形，point-in-polygon（射线法）零外部依赖。
// 边界数据 data/cities.json 由 DataV.GeoAtlas（可再分发公开数据）于 2026-09 生成：
// 地级市外环、Douglas-Peucker 简化 0.01°、坐标取 2 位小数（约 1km 精度，足够城市归属判定）。
package geo

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

//go:embed data/cities.json
var citiesJSON []byte

type ring struct {
	pts  [][]float64 // [lng, lat]
	bbox [4]float64  // minX, minY, maxX, maxY
}

type Index struct {
	byName map[string][]ring
	names  []string // 排序后遍历，保证边界重叠时结果确定
}

// Default 进程级单例（数据随二进制内嵌，仅解析一次）。
var (
	defaultOnce sync.Once
	defaultIdx  *Index
	defaultErr  error
)

func Default() (*Index, error) {
	defaultOnce.Do(func() { defaultIdx, defaultErr = New() })
	return defaultIdx, defaultErr
}

// New 解析内嵌边界数据构建判定索引。
func New() (*Index, error) {
	var raw map[string][][][]float64 // 城市名 → 多个外环 → [lng, lat]
	if err := json.Unmarshal(citiesJSON, &raw); err != nil {
		return nil, err
	}
	ix := &Index{byName: make(map[string][]ring, len(raw))}
	for name, rings := range raw {
		for _, pts := range rings {
			if len(pts) < 4 {
				continue
			}
			r := ring{pts: pts}
			r.bbox = bboxOf(pts)
			ix.byName[name] = append(ix.byName[name], r)
		}
	}
	for name := range ix.byName {
		ix.names = append(ix.names, name)
	}
	sort.Strings(ix.names)
	return ix, nil
}

// Locate 由坐标判定地级市名（短名，无「市」后缀）；未命中返回空串。
func (ix *Index) Locate(lat, lng float64) string {
	if ix == nil {
		return ""
	}
	for _, name := range ix.names {
		for _, r := range ix.byName[name] {
			if lng < r.bbox[0] || lng > r.bbox[2] || lat < r.bbox[1] || lat > r.bbox[3] {
				continue
			}
			if pointInRing(lat, lng, r.pts) {
				return name
			}
		}
	}
	return ""
}

// FallbackCity 城市判定的名称回退：取「厦门·鼓浪屿」式命名的「·」前缀作为城市；
// 无「·」的自由命名返回空串（避免把「家里」这类名称计入城市列表）。
func FallbackCity(name string) string {
	if i := strings.Index(name, "·"); i > 0 {
		return name[:i]
	}
	return ""
}

// pointInRing 射线法（even-odd）：环内（含边界邻域）返回 true。
func pointInRing(lat, lng float64, ringPts [][]float64) bool {
	inside := false
	j := len(ringPts) - 1
	for i := 0; i < len(ringPts); i++ {
		xi, yi := ringPts[i][0], ringPts[i][1]
		xj, yj := ringPts[j][0], ringPts[j][1]
		if (yi > lat) != (yj > lat) && lng < (xj-xi)*(lat-yi)/(yj-yi)+xi {
			inside = !inside
		}
		j = i
	}
	return inside
}

func bboxOf(pts [][]float64) [4]float64 {
	minX, minY, maxX, maxY := pts[0][0], pts[0][1], pts[0][0], pts[0][1]
	for _, p := range pts[1:] {
		if p[0] < minX {
			minX = p[0]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	return [4]float64{minX, minY, maxX, maxY}
}
