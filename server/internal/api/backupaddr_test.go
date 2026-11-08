package api

import (
	"testing"
)

func TestValidateBackupURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"合法 HTTPS", "https://my.example.com", "https://my.example.com", false},
		{"去掉尾斜杠", "https://my.example.com/", "https://my.example.com", false},
		{"带端口", "https://my.example.com:8443", "https://my.example.com:8443", false},
		{"带路径", "https://my.example.com/api", "https://my.example.com/api", false},
		{"拒绝 HTTP", "http://my.example.com", "", true},
		{"拒绝空串", "", "", true},
		{"拒绝缺域名", "https://", "", true},
		{"拒绝非 URL", "not a url", "", true},
		{"拒绝 ftp", "ftp://my.example.com", "", true},
		{"内网地址也合法", "https://192.168.1.10:8443", "https://192.168.1.10:8443", false},
		{"公网 IP 合法", "https://8.8.8.8", "https://8.8.8.8", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := validateBackupURL(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际通过（%s）", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestNewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newID("ba")
		if seen[id] {
			t.Fatalf("生成了重复 ID: %s", id)
		}
		seen[id] = true
	}
}
