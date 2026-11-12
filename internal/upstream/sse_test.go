package upstream

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrepareBodyForcesStream(t *testing.T) {
	out := PrepareBodyOpt([]byte(`{"model":"glm-5.2","messages":[]}`), true)
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["stream"] != true {
		t.Errorf("stream=%v", m["stream"])
	}
}

func TestPrepareBodyToolChoiceFunctionObject(t *testing.T) {
	out := PrepareBodyOpt([]byte(`{"tool_choice":{"type":"function","function":{"name":"get_weather"}},"tools":[{"type":"function"}]}`), true)
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["tool_choice"] != "get_weather" {
		t.Errorf("tool_choice=%v", m["tool_choice"])
	}
	if _, ok := m["tools"]; !ok {
		t.Error("tools should be kept for function choice")
	}
}

func TestPrepareBodyToolChoiceNone(t *testing.T) {
	for _, in := range []string{
		`{"tool_choice":"none","tools":[{}],"functions":[{}]}`,
		`{"tool_choice":{"type":"none"},"tools":[{}]}`,
	} {
		out := PrepareBodyOpt([]byte(in), true)
		var m map[string]any
		json.Unmarshal(out, &m)
		if _, ok := m["tool_choice"]; ok {
			t.Errorf("%s: tool_choice should be deleted", in)
		}
		if _, ok := m["tools"]; ok {
			t.Errorf("%s: tools should be deleted", in)
		}
		if _, ok := m["functions"]; ok {
			t.Errorf("%s: functions should be deleted", in)
		}
	}
}

func TestPrepareBodyToolChoiceAuto(t *testing.T) {
	out := PrepareBodyOpt([]byte(`{"tool_choice":{"type":"auto"}}`), true)
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["tool_choice"] != "auto" {
		t.Errorf("tool_choice=%v", m["tool_choice"])
	}
}

func TestPrepareBodyInvalidJSON(t *testing.T) {
	in := []byte(`{broken`)
	out := PrepareBodyOpt(in, true)
	if string(out) != string(in) {
		t.Error("invalid json should pass through unchanged")
	}
}

const sseFixture = "data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1753600000,\"model\":\"glm-5.2\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你好\"}}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1753600000,\"model\":\"glm-5.2\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"，世界\"}}]}\n\n" +
	"data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1753600000,\"model\":\"glm-5.2\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
	"data: [DONE]\n\n"

func TestAggregate(t *testing.T) {
	resp, err := Aggregate(strings.NewReader(sseFixture))
	if err != nil {
		t.Fatal(err)
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object=%v", resp["object"])
	}
	if resp["model"] != "glm-5.2" {
		t.Errorf("model=%v", resp["model"])
	}
	choices := resp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "你好，世界" {
		t.Errorf("content=%q", msg["content"])
	}
	if msg["role"] != "assistant" {
		t.Errorf("role=%v", msg["role"])
	}
	if choices[0].(map[string]any)["finish_reason"] != "stop" {
		t.Errorf("finish_reason=%v", choices[0].(map[string]any)["finish_reason"])
	}
	usage := resp["usage"].(map[string]any)
	if usage["total_tokens"].(float64) != 7 {
		t.Errorf("usage=%v", usage)
	}
}

func TestAggregateSkipsNonDataLines(t *testing.T) {
	raw := ": comment\n\n" + sseFixture
	resp, err := Aggregate(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "你好，世界" {
		t.Errorf("content=%q", msg["content"])
	}
}

func TestAggregateToolCalls(t *testing.T) {
	// 流式 tool_calls：首片带 id/type/name + 空 arguments，后续只带 arguments 片段
	raw := `data: {"id":"x1","model":"deepseek-v4-pro","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":"","tool_calls":[{"id":"call_a","type":"function","function":{"name":"get_weather","arguments":""},"index":0}]}}],"usage":null}

data: {"id":"x1","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"{\"city\":"},"index":0}]}}]}

data: {"id":"x1","choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"\"北京\"}"},"index":0}]}}]}

data: {"id":"x1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"total_tokens":11}}

data: [DONE]

`
	resp, err := Aggregate(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason=%v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	calls, ok := msg["tool_calls"].([]map[string]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls=%#v", msg["tool_calls"])
	}
	if calls[0]["id"] != "call_a" || calls[0]["type"] != "function" {
		t.Errorf("call meta=%v", calls[0])
	}
	fn := calls[0]["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("fn.name=%v", fn["name"])
	}
	if fn["arguments"] != `{"city":"北京"}` {
		t.Errorf("fn.arguments=%q", fn["arguments"])
	}
}

// TestNormalizeToolCallDeltas 直测分片规范化：首片保留 name；后续片的空 name 被删除，
// 且绝不主动补 name（按 name 判定新调用的客户端会因此截断 arguments）。
func TestNormalizeToolCallDeltas(t *testing.T) {
	mkFrame := func(name, args string) map[string]any {
		fn := map[string]any{}
		if name != "" {
			fn["name"] = name
		}
		if args != "" {
			fn["arguments"] = args
		}
		return map[string]any{"choices": []any{
			map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0.0, "function": fn},
			}}},
		}}
	}

	// 首片带 name：原样保留
	f0 := mkFrame("lookup", "")
	normalizeToolCallDeltas(f0)
	fn0 := f0["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if fn0["name"] != "lookup" {
		t.Fatalf("first chunk name=%v want lookup", fn0["name"])
	}

	// 后续片 name 为空串：删除该字段，不回填
	f1 := mkFrame("", `{"term":"x"}`)
	normalizeToolCallDeltas(f1)
	fn1 := f1["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if _, ok := fn1["name"]; ok {
		t.Errorf("follow-up chunk must not carry name: %v", fn1)
	}
	if fn1["arguments"] != `{"term":"x"}` {
		t.Errorf("arguments altered: %v", fn1["arguments"])
	}

	// 后续片本就缺省 name：保持缺省，不新增
	f2 := map[string]any{"choices": []any{
		map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": 0.0, "function": map[string]any{"arguments": "y"}},
		}}},
	}}
	normalizeToolCallDeltas(f2)
	fn2 := f2["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if _, ok := fn2["name"]; ok {
		t.Errorf("missing name must stay missing: %v", fn2)
	}

	// 不同 index 互不串扰：各自的空 name 都被删除
	f3 := map[string]any{"choices": []any{
		map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{"index": 1.0, "function": map[string]any{"name": "", "arguments": "z"}},
		}}},
	}}
	normalizeToolCallDeltas(f3)
	fn3 := f3["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if _, ok := fn3["name"]; ok {
		t.Errorf("index 1 empty name should be dropped: %v", fn3)
	}

	// 非 tool_calls 帧（content only）零影响
	f5 := map[string]any{"choices": []any{
		map[string]any{"delta": map[string]any{"content": "hi"}},
	}}
	normalizeToolCallDeltas(f5)
	if got := f5["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any); len(got) != 1 || got["content"] != "hi" {
		t.Errorf("content-only frame altered: %#v", got)
	}
}

// TestStreamNormalizesToolCallDeltas 端到端：SSE 流首片带 name，后续片 name 被置空，
// 透传后只有首片保留 name，后续片仅带 arguments 片段（OpenAI 流式协议）。
func TestStreamNormalizesToolCallDeltas(t *testing.T) {
	raw := "data: {\"id\":\"x1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"\",\"arguments\":\"{\\\"term\\\":\"}}]}}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"\",\"arguments\":\"\\\"北京\\\"}\"}}]}}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"total_tokens\":9}}\n\n" +
		"data: [DONE]\n\n"

	frames, done := streamFrames(t, raw)
	if done != 1 {
		t.Fatalf("done=%d want 1", done)
	}
	if len(frames) != 4 {
		t.Fatalf("frames=%d want 4", len(frames))
	}

	type deltaCall struct {
		frame   int
		hasName bool
		name    string
		args    string
	}
	var calls []deltaCall
	for i, fr := range frames {
		chs, ok := fr["choices"].([]any)
		if !ok {
			t.Fatalf("frame %d choices missing", i)
		}
		d, _ := chs[0].(map[string]any)["delta"].(map[string]any)
		tcs, ok := d["tool_calls"].([]any)
		if !ok {
			continue // 末帧只有 finish_reason，无 tool_calls 属正常
		}
		for _, tci := range tcs {
			tc := tci.(map[string]any)
			fn, _ := tc["function"].(map[string]any)
			c := deltaCall{frame: i}
			if fn != nil {
				c.name, _ = fn["name"].(string)
				_, c.hasName = fn["name"]
				c.args, _ = fn["arguments"].(string)
			}
			calls = append(calls, c)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("tool_calls 分片数=%d want 3", len(calls))
	}
	if !calls[0].hasName || calls[0].name != "lookup" {
		t.Errorf("首片必须带 name: %+v", calls[0])
	}
	for i := 1; i < len(calls); i++ {
		if calls[i].hasName {
			t.Errorf("frame %d 后续片不应带 name=%q（会导致 arguments 缓冲被重置）", calls[i].frame, calls[i].name)
		}
	}
	if calls[1].args != `{"term":` || calls[2].args != `"北京"}` {
		t.Errorf("arguments 片段未完整保留: %+v", calls)
	}
}

// streamFrames 把原始 SSE 输入经 Stream 处理后解析出所有 JSON 帧及 [DONE] 计数。
func streamFrames(t *testing.T, raw string) (frames []map[string]any, doneCount int) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := Stream(rec, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "data: [DONE]") {
			doneCount++
			continue
		}
		if strings.HasPrefix(ln, "data: ") {
			var obj map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(ln, "data: ")), &obj); err != nil {
				t.Fatalf("bad frame %q: %v", ln, err)
			}
			frames = append(frames, obj)
		}
	}
	return frames, doneCount
}

func TestNormalizeFrame(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string // 规范化后 marshal 的期望 JSON（Go map 键按字典序输出）
	}{
		{"empty content/refusal and finish_reason empty string",
			map[string]any{"id": "x", "choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"content": "", "refusal": ""}, "finish_reason": ""},
			}},
			`{"choices":[{"delta":{},"finish_reason":null,"index":0}],"id":"x","object":"chat.completion.chunk"}`},
		{"non-empty tool_calls kept",
			map[string]any{"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"id": "c1", "type": "function"}}}},
			}},
			`{"choices":[{"delta":{"tool_calls":[{"id":"c1","type":"function"}]},"finish_reason":null,"index":0}],"id":"chatcmpl-wb2api","object":"chat.completion.chunk"}`},
		{"empty tool_calls list dropped",
			map[string]any{"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{}, "content": "hi"}},
			}},
			`{"choices":[{"delta":{"content":"hi"},"finish_reason":null,"index":0}],"id":"chatcmpl-wb2api","object":"chat.completion.chunk"}`},
		{"empty placeholder function_call dropped",
			map[string]any{"choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{"function_call": map[string]any{"name": "", "arguments": ""}}},
			}},
			`{"choices":[{"delta":{},"finish_reason":null,"index":0}],"id":"chatcmpl-wb2api","object":"chat.completion.chunk"}`},
		{"top-level unknown fields dropped, usage omitted when absent",
			map[string]any{"id": "x", "object": "chat.completion.chunk", "created": 1, "junk": "noise", "choices": []any{
				map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
			}},
			`{"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":1,"id":"x","object":"chat.completion.chunk"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(normalizeFrame(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != c.want {
				t.Errorf("got  %s\nwant %s", raw, c.want)
			}
		})
	}
}

func TestStreamNormalizesFrames(t *testing.T) {
	// 混合噪声帧：空 content/reasoning/refusal/function_call + 空 tool_calls + 顶层非标字段，
	// 随后非空 content + tool_calls 帧，最后 finish/usage 帧。
	raw := "data: {\"id\":\"x1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"reasoning_content\":\"\",\"refusal\":\"\",\"tool_calls\":[],\"function_call\":{\"name\":\"\",\"arguments\":\"\"}},\"finish_reason\":\"\"}],\"extra_field\":\"junk\"}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\",\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"北京\\\"}\"},\"index\":0}]},\"finish_reason\":\"\"}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"

	frames, done := streamFrames(t, raw)
	if done != 1 {
		t.Fatalf("done frames=%d want 1", done)
	}
	if len(frames) != 3 {
		t.Fatalf("frames=%d want 3", len(frames))
	}

	// 帧 1：噪声全剔除，finish_reason ""→null，usage 缺失→不输出该键，顶层非标字段剥除
	f0 := frames[0]
	if _, ok := f0["extra_field"]; ok {
		t.Error("top-level extra_field should be dropped")
	}
	if _, ok := f0["usage"]; ok {
		t.Errorf("usage key must be absent on intermediate frames, got %v", f0["usage"])
	}
	ch0 := f0["choices"].([]any)[0].(map[string]any)
	if ch0["finish_reason"] != nil {
		t.Errorf("frame1 finish_reason=%v want null", ch0["finish_reason"])
	}
	d := ch0["delta"].(map[string]any)
	// role 是合法白名单键保留；空 content/reasoning/refusal/tool_calls/function_call 噪声全剔除
	if len(d) != 1 || d["role"] != "assistant" {
		t.Errorf("frame1 delta should only keep role, got %#v", d)
	}
	for _, noise := range []string{"content", "reasoning_content", "refusal", "tool_calls", "function_call"} {
		if _, ok := d[noise]; ok {
			t.Errorf("frame1 delta should drop %q, got %#v", noise, d)
		}
	}

	// 帧 2：非空 content 与 tool_calls 保留，finish_reason ""→null
	f1 := frames[1]
	ch1 := f1["choices"].([]any)[0].(map[string]any)
	d1 := ch1["delta"].(map[string]any)
	if d1["content"] != "hello" {
		t.Errorf("frame2 content=%v", d1["content"])
	}
	tcs, ok := d1["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("frame2 tool_calls=%#v", d1["tool_calls"])
	}
	if ch1["finish_reason"] != nil {
		t.Errorf("frame2 finish_reason=%v want null (input empty string)", ch1["finish_reason"])
	}

	// 帧 3：finish_reason 非空保留，usage 保留
	f2 := frames[2]
	ch2 := f2["choices"].([]any)[0].(map[string]any)
	if ch2["finish_reason"] != "stop" {
		t.Errorf("frame3 finish_reason=%v want stop", ch2["finish_reason"])
	}
	if f2["usage"].(map[string]any)["total_tokens"].(float64) != 7 {
		t.Errorf("frame3 usage=%v", f2["usage"])
	}
}

// TestStreamFirstIdPassthrough 帧混合（首帧有 id / 中间帧无 id / 空串 id）：输出每帧 id
// 必须连续一致（取首帧真实值），不再一律 chatcmpl-wb2api（issue #35 后台聚合：透传流里
// 每帧同 id 才能按消息归并）。
func TestStreamFirstIdPassthrough(t *testing.T) {
	raw := "data: {\"id\":\"chatcmpl-upstream-9\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n" +
		// 中间帧无 id：应复用首帧 id。
		"data: {\"object\":\"chat.completion.chunk\",\"created\":1,\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"}}]}\n\n" +
		// 中间帧 id 为空串：同样复用首帧 id。
		"data: {\"id\":\"\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	frames, done := streamFrames(t, raw)
	if done != 1 {
		t.Fatalf("done=%d want 1", done)
	}
	if len(frames) != 3 {
		t.Fatalf("frames=%d want 3", len(frames))
	}
	for i, fr := range frames {
		if got := fr["id"]; got != "chatcmpl-upstream-9" {
			t.Errorf("frame %d id=%v want chatcmpl-upstream-9 (首帧真实 id 续传)", i, got)
		}
	}
}

// TestStreamNoIdFallsBackToSentinel 全流无任何真实 id → 兜底 chatcmpl-wb2api
// （整流无 id 时的既有哨兵，帧与帧之间仍一惯性存在）。
func TestStreamNoIdFallsBackToSentinel(t *testing.T) {
	raw := "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{}," +
		"\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	frames, _ := streamFrames(t, raw)
	if len(frames) != 2 {
		t.Fatalf("frames=%d want 2", len(frames))
	}
	for i, fr := range frames {
		if got := fr["id"]; got != "chatcmpl-wb2api" {
			t.Errorf("frame %d id=%v want sentinel chatcmpl-wb2api (无真实 id)", i, got)
		}
	}
}

func TestStreamDoneFallback(t *testing.T) {
	// 上游流在无 [DONE] 时 EOF，Stream 必须兜底写一个 [DONE]
	rec := httptest.NewRecorder()
	err := Stream(rec, strings.NewReader("data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.HasSuffix(strings.TrimRight(body, "\n"), "data: [DONE]") {
		t.Errorf("missing [DONE] fallback: %q", body)
	}

	// 已有 [DONE] 时只写一次，不重复
	rec2 := httptest.NewRecorder()
	if err := Stream(rec2, strings.NewReader("data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(rec2.Body.String(), "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count=%d want 1: %q", n, rec2.Body.String())
	}
}

func TestStreamPassthrough(t *testing.T) {
	rec := httptest.NewRecorder()
	err := Stream(rec, strings.NewReader(sseFixture))
	if err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "你好") || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("body missing chunks: %q", body)
	}
	// 逐行仍是合法 SSE（每行以 data: 开头或是空行）
	for _, ln := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if ln != "" && !strings.HasPrefix(ln, "data: ") {
			t.Errorf("bad line: %q", ln)
		}
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type=%q", ct)
	}
}

// TestAggregateEmptyStreamCases 覆盖空流检测：0 有效事件必须报错、[DONE] 即 break、
// [DONE] 后垃圾不进聚合、正常聚合回归。
func TestAggregateEmptyStreamCases(t *testing.T) {
	// 正常回归基流：content + finish_reason + usage，[DONE] 收尾。
	valid := "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm-5.2\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"}}]}\n\n" +
		"data: {\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"

	cases := []struct {
		name    string
		raw     string
		wantErr bool
		// 回归断言（仅在 wantErr=false 时校验）
		wantContent string
		wantUsage   float64
	}{
		{
			name:    "空流（EOF 即止）",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "只有注释行和空行加 DONE",
			raw:     ": comment\n\n: another comment\n\ndata: [DONE]\n\n",
			wantErr: true,
		},
		{
			name:    "DONE 后跟垃圾帧不进聚合",
			raw:     valid[:len(valid)-len("data: [DONE]\n\n")] + "data: [DONE]\n\ndata: {\"junk\":\"should not aggregate\"}\n\n",
			wantErr: false,
			// 与 valid 基流一致的聚合期望
			wantContent: "hi",
			wantUsage:   7,
		},
		{
			name:        "正常流回归",
			raw:         valid,
			wantErr:     false,
			wantContent: "hi",
			wantUsage:   7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := Aggregate(strings.NewReader(c.raw))
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (resp=%v)", resp)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
			if msg["content"] != c.wantContent {
				t.Errorf("content=%q want %q", msg["content"], c.wantContent)
			}
			if u, ok := resp["usage"].(map[string]any); ok {
				if u["total_tokens"].(float64) != c.wantUsage {
					t.Errorf("usage=%v want %v", u["total_tokens"], c.wantUsage)
				}
			} else {
				t.Errorf("usage missing")
			}
		})
	}
}

// TestAggregateEmptyStreamError 校验空流错误信息形如约定文案。
func TestAggregateEmptyStreamError(t *testing.T) {
	_, err := Aggregate(strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "no valid data events") {
		t.Fatalf("err=%v", err)
	}
}

// TestStreamEmptyFramesCase 覆盖流式空流检测：0 有效帧时写 error 帧（error 字段存活）,
// 恰好一个 [DONE]，并返回非 nil error。
func TestStreamEmptyFramesCase(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"空流", ""},
		{"只有注释行", ": comment\n\n"},
		{"只有 DONE", "data: [DONE]\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := Stream(rec, strings.NewReader(c.raw))
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			body := rec.Body.String()
			if n := strings.Count(body, "data: [DONE]"); n != 1 {
				t.Errorf("[DONE] count=%d want 1: %q", n, body)
			}
			// error 帧必须原样保留 error 字段（未被 normalizeFrame 白名单剥掉）
			var e map[string]any
			found := false
			for _, ln := range strings.Split(body, "\n") {
				ln = strings.TrimSpace(ln)
				if strings.HasPrefix(ln, "data: ") {
					payload := strings.TrimPrefix(ln, "data: ")
					if payload == "[DONE]" {
						continue
					}
					if json.Unmarshal([]byte(payload), &e) == nil {
						if em, ok := e["error"].(map[string]any); ok && em["message"] == "empty upstream stream" && em["type"] == "upstream_error" {
							found = true
						}
					}
				}
			}
			if !found {
				t.Errorf("error frame absent or error field stripped: %q", body)
			}
		})
	}
}

// TestStreamGarbageAfterDone 校验 DONE 之后的垃圾帧不出现在响应里。
func TestStreamGarbageAfterDone(t *testing.T) {
	raw := "data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: [DONE]\n\n" +
		"data: {\"should\":\"not appear\"}\n\n"
	rec := httptest.NewRecorder()
	if err := Stream(rec, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "should") {
		t.Errorf("garbage after DONE leaked into response: %q", body)
	}
	if n := strings.Count(body, "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count=%d want 1: %q", n, body)
	}
	// 有效帧仍被透传
	if !strings.Contains(body, "hello") {
		t.Errorf("valid frame missing: %q", body)
	}
}

// TestStreamNormalPassthroughRegression 校验正常透传回归：帧被 normalize 后透传、
// 末尾恰好一个 [DONE]、无 error 帧；上游漏发 DONE 时自动补。
func TestStreamNormalPassthroughRegression(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"带 DONE 的正常流", sseFixture},
		{"漏发 DONE 自动补", "data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := Stream(rec, strings.NewReader(c.raw)); err != nil {
				t.Fatal(err)
			}
			body := rec.Body.String()
			if strings.Contains(body, `"error"`) {
				t.Errorf("unexpected error frame: %q", body)
			}
			if n := strings.Count(body, "data: [DONE]"); n != 1 {
				t.Errorf("[DONE] count=%d want 1: %q", n, body)
			}
			// 帧被规范化：含 "id" 且有标准 object 字段
			if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
				t.Errorf("frame not normalized: %q", body)
			}
		})
	}
}

// TestNormalizeFrameClientCompatShape 覆盖 AI agent / IDE 客户端按 chunk 逐帧解析时
// 必需的字段形态：choices/delta/index 恒存在、usage 只在末帧出现、error 帧可识别。
func TestNormalizeFrameClientCompatShape(t *testing.T) {
	t.Run("usage-only frame keeps empty choices", func(t *testing.T) {
		out := normalizeFrame(map[string]any{"id": "x", "usage": map[string]any{"total_tokens": 7}})
		chs, ok := out["choices"].([]any)
		if !ok || len(chs) != 0 {
			t.Fatalf("choices=%#v want empty array", out["choices"])
		}
		if out["usage"] == nil {
			t.Error("usage must be preserved when upstream provides it")
		}
	})

	t.Run("intermediate frame omits usage key", func(t *testing.T) {
		out := normalizeFrame(map[string]any{"choices": []any{
			map[string]any{"delta": map[string]any{"content": "hi"}},
		}})
		if _, ok := out["usage"]; ok {
			t.Errorf("intermediate frame must not carry usage, got %v", out["usage"])
		}
		ch := out["choices"].([]any)[0].(map[string]any)
		if ch["index"] != 0 {
			t.Errorf("index=%v want 0", ch["index"])
		}
		if _, ok := ch["delta"]; !ok {
			t.Error("delta must be present")
		}
		if _, ok := ch["finish_reason"]; !ok {
			t.Error("finish_reason must be present")
		}
	})

	t.Run("upstream error field survives whitelist", func(t *testing.T) {
		errObj := map[string]any{"message": "upstream refused", "type": "upstream_error"}
		out := normalizeFrame(map[string]any{"id": "x", "error": errObj})
		got, ok := out["error"].(map[string]any)
		if !ok || got["message"] != "upstream refused" {
			t.Fatalf("error field stripped: %#v", out)
		}
		if _, ok := out["choices"]; !ok {
			t.Error("choices must be present even on error frames")
		}
	})
}

// TestStreamClientCompatFrames 校验真实流式输出：中间帧无 usage 键、注释行不透传、
// choices 恒存在、[DONE] 恰好一个、末帧 usage 保留。
func TestStreamClientCompatFrames(t *testing.T) {
	raw := ": heartbeat\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"id\":\"x1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	if err := Stream(rec, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "heartbeat") {
		t.Errorf("upstream comment line leaked: %q", body)
	}
	if n := strings.Count(body, "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count=%d want 1", n)
	}
	var frames []map[string]any
	for _, ln := range strings.Split(body, "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "data: ") || strings.TrimPrefix(ln, "data: ") == "[DONE]" {
			continue
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(ln, "data: ")), &f); err != nil {
			t.Fatalf("bad frame %q: %v", ln, err)
		}
		frames = append(frames, f)
		if _, ok := f["choices"]; !ok {
			t.Errorf("frame missing choices: %v", f)
		}
	}
	if len(frames) != 3 {
		t.Fatalf("frames=%d want 3", len(frames))
	}
	for i := 0; i < 2; i++ {
		if _, ok := frames[i]["usage"]; ok {
			t.Errorf("frame %d carries usage (must be omitted): %v", i, frames[i]["usage"])
		}
	}
	if frames[2]["usage"] == nil {
		t.Error("final frame lost usage object")
	}
}

// TestStreamKeepsUpstreamErrorFrame 校验上游以 200 + SSE error 帧报错时，
// error 字段不被白名单剥成空帧（客户端才能显示具体错误而非泛化异常）。
func TestStreamKeepsUpstreamErrorFrame(t *testing.T) {
	raw := "data: {\"id\":\"x1\",\"error\":{\"message\":\"upstream refused\",\"type\":\"upstream_error\"}}\n\n" +
		"data: [DONE]\n\n"
	rec := httptest.NewRecorder()
	if err := Stream(rec, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "upstream refused") {
		t.Errorf("error field stripped: %q", body)
	}
	if strings.Contains(body, "empty upstream stream") {
		t.Errorf("valid error frame must not trigger empty-stream fallback: %q", body)
	}
	if n := strings.Count(body, "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count=%d want 1", n)
	}
}
