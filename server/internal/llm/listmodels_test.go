package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runFakeUpstream 起一个按断言返回固定响应体的假上游，并通过 *got 回传收到的请求。
func runFakeUpstream(t *testing.T, status int, body string) (*httptest.Server, **http.Request) {
	t.Helper()
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestListModelsProtocols(t *testing.T) {
	tests := []struct {
		name     string
		apiType  string
		body     string
		wantURL  string
		wantAuth string // Authorization 或 x-api-key 或 x-goog-api-key
		wantIDs  []string
	}{
		{
			name:     "openai-chat",
			apiType:  "openai-chat",
			body:     `{"data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`,
			wantURL:  "/models",
			wantAuth: "Bearer sk-test",
			wantIDs:  []string{"gpt-4o", "gpt-4o-mini"},
		},
		{
			name:     "openai-responses",
			apiType:  "openai-responses",
			body:     `{"data":[{"id":"o1"}]}`,
			wantURL:  "/models",
			wantAuth: "Bearer sk-test",
			wantIDs:  []string{"o1"},
		},
		{
			name:     "anthropic",
			apiType:  "anthropic",
			body:     `{"data":[{"id":"claude-sonnet"},{"id":"claude-haiku"}]}`,
			wantURL:  "/v1/models",
			wantAuth: "x-api-key=sk-test",
			wantIDs:  []string{"claude-sonnet", "claude-haiku"},
		},
		{
			name:     "gemini",
			apiType:  "gemini",
			body:     `{"models":[{"name":"models/gemini-1.5-pro"},{"name":"models/gemini-1.5-flash"}]}`,
			wantURL:  "/v1beta/models",
			wantAuth: "x-goog-api-key=sk-test",
			wantIDs:  []string{"gemini-1.5-pro", "gemini-1.5-flash"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, got := runFakeUpstream(t, http.StatusOK, tc.body)

			ids, err := ListModels(context.Background(), tc.apiType, srv.URL, "sk-test")
			if err != nil {
				t.Fatalf("ListModels: %v", err)
			}
			if len(ids) != len(tc.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, tc.wantIDs)
			}
			for i, want := range tc.wantIDs {
				if ids[i] != want {
					t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
				}
			}

			if got == nil || *got == nil {
				t.Fatal("upstream request not captured")
			}
			r := *got
			if r.URL.Path != tc.wantURL {
				t.Errorf("upstream path = %q, want %q", r.URL.Path, tc.wantURL)
			}
			// Gemini 不允许把 Key 放进 URL query string（会落代理/上游日志）
			if r.URL.Query().Get("key") != "" {
				t.Errorf("gemini must not carry api key in query string: %q", r.URL.String())
			}
			// 鉴权头校验（统一在请求头里传 Key）
			switch {
			case strings.HasPrefix(tc.wantAuth, "Bearer "):
				if r.Header.Get("Authorization") != tc.wantAuth {
					t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), tc.wantAuth)
				}
			case strings.HasPrefix(tc.wantAuth, "x-api-key="):
				want := strings.TrimPrefix(tc.wantAuth, "x-api-key=")
				if r.Header.Get("x-api-key") != want {
					t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), want)
				}
				if tc.apiType == "anthropic" && r.Header.Get("anthropic-version") == "" {
					t.Error("anthropic-version header missing")
				}
			case strings.HasPrefix(tc.wantAuth, "x-goog-api-key="):
				want := strings.TrimPrefix(tc.wantAuth, "x-goog-api-key=")
				if r.Header.Get("x-goog-api-key") != want {
					t.Errorf("x-goog-api-key = %q, want %q", r.Header.Get("x-goog-api-key"), want)
				}
			}
		})
	}
}

func TestListModelsErrors(t *testing.T) {
	t.Run("unsupported_api_type", func(t *testing.T) {
		srv, _ := runFakeUpstream(t, http.StatusOK, `{}`)
		_, err := ListModels(context.Background(), "bogus", srv.URL, "k")
		if err == nil || !strings.Contains(err.Error(), "api_type 不支持") {
			t.Fatalf("err = %v, want api_type 不支持", err)
		}
	})

	t.Run("upstream_401_wrapped", func(t *testing.T) {
		srv, _ := runFakeUpstream(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
		_, err := ListModels(context.Background(), "openai-chat", srv.URL, "bad")
		if err == nil {
			t.Fatal("want error for 401")
		}
		// 非 2xx → 包含 HTTP 状态与上游片段（AD1 fetch-models 的 502 包装在 handler 层）
		if !strings.Contains(err.Error(), "HTTP 401") {
			t.Errorf("err = %v, want HTTP 401 marker", err)
		}
	})

	t.Run("gemini_key_not_in_url", func(t *testing.T) {
		srv, got := runFakeUpstream(t, http.StatusOK, `{"models":[]}`)
		_, err := ListModels(context.Background(), "gemini", srv.URL, "secret")
		if err != nil {
			t.Fatalf("ListModels: %v", err)
		}
		if got == nil || *got == nil {
			t.Fatal("upstream request not captured")
		}
		r := *got
		if strings.Contains(r.URL.String(), "secret") {
			t.Errorf("api key leaked into URL: %q", r.URL.String())
		}
		if r.Header.Get("x-goog-api-key") != "secret" {
			t.Errorf("x-goog-api-key = %q, want secret", r.Header.Get("x-goog-api-key"))
		}
	})
}
