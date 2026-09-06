package geo

import "strings"

// Place 离线地名候选（geocode 表；数据与 tools/mock/fixtures.mjs GEOCODE_PLACES 同源，
// 两端行为一致：包含匹配、命中取前 8 条）。
type Place struct {
	Name    string  `json:"name"`
	City    string  `json:"city"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Display string  `json:"display"`
}

var places = []Place{
	{Name: "杭州", City: "杭州", Lat: 30.2741, Lng: 120.1551, Display: "浙江·杭州"},
	{Name: "杭州·西湖", City: "杭州", Lat: 30.2467, Lng: 120.4599, Display: "浙江·杭州·西湖"},
	{Name: "上海", City: "上海", Lat: 31.2304, Lng: 121.4737, Display: "上海·上海"},
	{Name: "上海·外滩", City: "上海", Lat: 31.2397, Lng: 121.4903, Display: "上海·外滩"},
	{Name: "北京", City: "北京", Lat: 39.9042, Lng: 116.4074, Display: "北京·北京"},
	{Name: "广州", City: "广州", Lat: 23.1291, Lng: 113.2644, Display: "广东·广州"},
	{Name: "深圳", City: "深圳", Lat: 22.5431, Lng: 114.0579, Display: "广东·深圳"},
	{Name: "成都", City: "成都", Lat: 30.5728, Lng: 104.0668, Display: "四川·成都"},
	{Name: "武汉", City: "武汉", Lat: 30.5928, Lng: 114.3055, Display: "湖北·武汉"},
	{Name: "西安", City: "西安", Lat: 34.3416, Lng: 108.9398, Display: "陕西·西安"},
	{Name: "重庆", City: "重庆", Lat: 29.563, Lng: 106.5516, Display: "重庆·重庆"},
	{Name: "南京", City: "南京", Lat: 32.0603, Lng: 118.7969, Display: "江苏·南京"},
	{Name: "苏州", City: "苏州", Lat: 31.2989, Lng: 120.5853, Display: "江苏·苏州"},
	{Name: "苏州·平江路", City: "苏州", Lat: 31.3097, Lng: 120.6309, Display: "江苏·苏州·平江路"},
	{Name: "厦门", City: "厦门", Lat: 24.4798, Lng: 118.0894, Display: "福建·厦门"},
	{Name: "厦门·鼓浪屿", City: "厦门", Lat: 24.4438, Lng: 118.0683, Display: "福建·厦门·鼓浪屿"},
	{Name: "厦门·曾厝垵", City: "厦门", Lat: 24.4305, Lng: 118.1121, Display: "福建·厦门·曾厝垵"},
	{Name: "大理", City: "大理", Lat: 25.6065, Lng: 100.2672, Display: "云南·大理"},
	{Name: "丽江", City: "丽江", Lat: 26.8721, Lng: 100.2299, Display: "云南·丽江"},
	{Name: "青岛", City: "青岛", Lat: 36.0671, Lng: 120.3826, Display: "山东·青岛"},
}

// SearchPlaces 地名搜索：name/city/display 包含匹配（大小写不敏感），取前 limit 条。
func SearchPlaces(q string, limit int) []Place {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var hits []Place
	for _, p := range places {
		if strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.City), q) ||
			strings.Contains(strings.ToLower(p.Display), q) {
			hits = append(hits, p)
			if len(hits) >= limit {
				break
			}
		}
	}
	return hits
}

// LocatePlace 地名 → 坐标回填（动态带地点但缺坐标时使用，对齐 mock locatePlace 行为）。
func LocatePlace(name string) (Place, bool) {
	hits := SearchPlaces(name, 1)
	if len(hits) == 0 {
		return Place{}, false
	}
	return hits[0], true
}
