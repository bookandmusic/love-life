package limits

import "testing"

// 共享上限的取值守卫：media.json 是 Go 与 APP 的共同数据源，被改坏时必须在**加载期**就报错，
// 而不是静默变成 0（上限为 0 会让「一个媒体都不许发」，比报错更难查）。
func TestMediaLimitsLoaded(t *testing.T) {
	// 只断言两条真实契约（正值由 mustParseMedia 在加载期 panic 守住，不在此重复）：
	// 与 PRODUCT 声明的用户可见上限一致；聊天引用与发动态同口径。
	if MaxMediaPerPost != 9 {
		t.Errorf("MaxMediaPerPost = %d，与 PRODUCT「发布动态」的「单条最多 9 个」不一致", MaxMediaPerPost)
	}
	if MaxMediaPerChatMessage != MaxMediaPerPost {
		t.Errorf("聊天引用上限 %d 应与发动态上限 %d 同口径", MaxMediaPerChatMessage, MaxMediaPerPost)
	}
}

func TestMustParseMediaRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"非法 JSON", "{not json"},
		{"缺字段", `{"maxMediaPerPost":9}`},
		{"零值", `{"maxMediaPerPost":0,"maxMediaPerChatMessage":9}`},
		{"负值", `{"maxMediaPerPost":-1,"maxMediaPerChatMessage":9}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("应 panic 而非静默接受: %s", c.in)
				}
			}()
			_ = mustParseMedia([]byte(c.in))
		})
	}
}
