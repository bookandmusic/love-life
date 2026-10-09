package llm

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// sseScanner 逐事件读取 SSE 流。四协议的上游都是「一帧一个 JSON」，且 anthropic /
// openai-responses 的载荷自带 type 字段，比事件名更可靠，故这里只交出 data 载荷、
// 不解析 event: 名——避免四份 adapter 各写一遍逐行解析（AI模式设计「模型网关」）。
type sseScanner struct {
	sc *bufio.Scanner
}

func newSSEScanner(r io.Reader) *sseScanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &sseScanner{sc: sc}
}

// next 返回下一个事件的 data 载荷（多行 data 按 SSE 规范以 \n 拼接）；流结束返回 ok=false。
// 注释行与 event:/id:/retry: 行按规范忽略；空帧（只有 event: 没有 data:）不产出事件。
func (s *sseScanner) next() (string, bool) {
	var data []string
	for s.sc.Scan() {
		line := s.sc.Text()
		if line == "" {
			if len(data) > 0 {
				return strings.Join(data, "\n"), true
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(rest, " "))
		}
	}
	return "", false
}

func (s *sseScanner) err() error { return s.sc.Err() }

// streamStatusError 把非 200 响应转成错误，200 返回 nil。
// 四协议上游的错误体形状一致（{"error":{"message":…}}），故共用一份解析：
// 各 adapter 各写一遍会得到四份逐字相同的代码，改错误措辞时又得改四处。
func streamStatusError(res *http.Response) error {
	if res.StatusCode == http.StatusOK {
		return nil
	}
	var out ChatResponse
	_ = json.NewDecoder(res.Body).Decode(&out)
	if out.Error != nil && out.Error.Message != "" {
		return upstreamMessage(res.StatusCode, out.Error.Message)
	}
	return upstreamStatus(res.StatusCode)
}

// sseJSON 解析一帧载荷；非 JSON（心跳、空帧、[DONE]）返回 false，由调用方跳过。
func sseJSON(data string, v any) bool {
	if data == "" || data == "[DONE]" {
		return false
	}
	return json.Unmarshal([]byte(data), v) == nil
}

// isEventStream 判断响应是否真的是事件流。
//
// 需要这个判断是因为兼容生态与中转网关普遍**忽略 stream:true 直接回整块 JSON**
// （OpenAI 兼容类尤其常见）。此时若照 SSE 解析，一个 data: 行都读不到，整轮会静默
// 变成空回答——「配了不生效」的典型形态。故四个协议都在此处兜底：
// 不是事件流就按整块响应解析，同一响应同一计费，不重发请求。
func isEventStream(res *http.Response) bool {
	return strings.Contains(res.Header.Get("Content-Type"), "text/event-stream")
}

// streamFallbackReplay 非事件流兜底：中转网关忽略 stream:true 直接回整块 JSON 时，
// 按整块响应解析并回放 Delta，不重发请求（同一响应、同一计费）。
// 三个协议只差解析函数，故由调用方传入，避免三份逐字相同的读体+解析+回放。
func streamFallbackReplay(res *http.Response, parse func([]byte, int) (*ChatResponse, error), onDelta func(Delta)) (string, error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	out, err := parse(body, res.StatusCode)
	if err != nil {
		return "", err
	}
	return replayChatResponse(out, onDelta), nil
}

// replayChatResponse 把整块响应拆成 Delta 回放（非事件流兜底路径共用）。
// 顺序与原生流式一致：思考 → 正文 → 工具调用 → 用量。
func replayChatResponse(out *ChatResponse, onDelta func(Delta)) string {
	if len(out.Choices) == 0 {
		return ""
	}
	m := out.Choices[0].Message
	if m.Reasoning != "" {
		onDelta(Delta{Reasoning: m.Reasoning})
	}
	if m.Content != "" {
		onDelta(Delta{Content: m.Content})
	}
	for i := range m.ToolCalls {
		onDelta(Delta{ToolCall: &ToolCallDelta{ID: m.ToolCalls[i].ID,
			Name:      m.ToolCalls[i].Function.Name,
			Arguments: m.ToolCalls[i].Function.Arguments,
			// 签名必须一起回放：中转网关回整块 JSON 时若把它丢掉，
			// gemini 3 下一轮就 400——兜底路径同样要保住凭据
			Signature: m.ToolCalls[i].Signature}})
	}
	u := out.Usage
	onDelta(Delta{Usage: &u})
	return out.Choices[0].FinishReason
}
