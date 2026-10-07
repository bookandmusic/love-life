package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIChatStreamReplayNonSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	var got []Delta
	finish, err := (openaiChatAdapter{}).StreamChat(context.Background(), srv.URL, "k", "m", &ChatRequest{}, func(d Delta) {
		got = append(got, d)
	})
	if err != nil {
		t.Fatal(err)
	}
	if finish != "stop" || len(got) == 0 || got[0].Content != "ok" {
		t.Fatalf("finish=%q deltas=%+v", finish, got)
	}
}
