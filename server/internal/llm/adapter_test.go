package llm

import (
	"errors"
	"testing"
)

func TestSelectAdapter(t *testing.T) {
	tests := []struct {
		name       string
		apiType    string
		wantOK     bool
		wantNotNil bool // wantOK=true 时断言 adapter != nil
	}{
		{name: "openai-chat 已实现", apiType: APITypeOpenAIChat, wantOK: true, wantNotNil: true},
		{name: "openai-responses 已实现", apiType: APITypeOpenAIResponses, wantOK: true, wantNotNil: true},
		{name: "anthropic 已实现", apiType: APITypeAnthropic, wantOK: true, wantNotNil: true},
		{name: "gemini 已实现", apiType: APITypeGemini, wantOK: true, wantNotNil: true},
		{name: "bogus 协议", apiType: "bogus", wantOK: false},
		{name: "空字符串", apiType: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adp, err := selectAdapter(tc.apiType)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("selectAdapter(%q) err = %v, want nil", tc.apiType, err)
				}
				if tc.wantNotNil && adp == nil {
					t.Fatal("adapter = nil, want non-nil")
				}
				return
			}
			if !errors.Is(err, ErrUnsupportedAPIType) {
				t.Fatalf("err = %v, want ErrUnsupportedAPIType", err)
			}
		})
	}
}

func TestNewForProtocol(t *testing.T) {
	t.Run("openai-chat 成功", func(t *testing.T) {
		c, err := NewForProtocol("https://x", "k", "m", APITypeOpenAIChat)
		if err != nil || c == nil {
			t.Fatalf("NewForProtocol: c=%v err=%v", c, err)
		}
		if c.apiType != APITypeOpenAIChat || c.adp == nil {
			t.Fatalf("client wiring broken: apiType=%q adp=%v", c.apiType, c.adp)
		}
	})

	t.Run("未实现协议返回错误", func(t *testing.T) {
		if _, err := NewForProtocol("https://x", "k", "m", "bogus"); !errors.Is(err, ErrUnsupportedAPIType) {
			t.Fatalf("err = %v, want ErrUnsupportedAPIType", err)
		}
	})

	t.Run("四协议全部可构造", func(t *testing.T) {
		for _, apiType := range []string{APITypeOpenAIChat, APITypeOpenAIResponses, APITypeAnthropic, APITypeGemini} {
			c, err := NewForProtocol("https://x", "k", "m", apiType)
			if err != nil || c == nil || c.adp == nil {
				t.Errorf("NewForProtocol(%q): c=%v err=%v", apiType, c, err)
			}
		}
	})

	t.Run("New 默认 openai-chat", func(t *testing.T) {
		c := New("https://x", "k", "m")
		if c.apiType != APITypeOpenAIChat || c.adp == nil {
			t.Fatalf("New default broken: apiType=%q adp=%v", c.apiType, c.adp)
		}
	})
}
