package geo

import "testing"

// 已知坐标取自 mock fixtures 的点位（tools/mock/fixtures.mjs mapPoints）。
func TestLocate(t *testing.T) {
	ix, err := New()
	if err != nil {
		t.Fatalf("new index: %v", err)
	}
	cases := []struct {
		name     string
		lat, lng float64
		want     string
	}{
		{"杭州西湖", 30.2592, 120.1483, "杭州"},
		{"厦门鼓浪屿", 24.4438, 118.0683, "厦门"},
		{"上海外滩", 31.2397, 121.4902, "上海"},
		{"北京", 39.9042, 116.4074, "北京"},
		{"海外", 0, 0, ""},
	}
	for _, c := range cases {
		if got := ix.Locate(c.lat, c.lng); got != c.want {
			t.Errorf("Locate(%v, %v) = %q, want %q", c.lat, c.lng, got, c.want)
		}
	}
}

func TestFallbackCity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"厦门·鼓浪屿", "厦门"},
		{"杭州·西湖", "杭州"},
		{"家里", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := FallbackCity(c.in); got != c.want {
			t.Errorf("FallbackCity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
