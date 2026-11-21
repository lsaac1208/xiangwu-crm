package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type ModelMessage struct {
	ReasoningContent *string    `json:"reasoning_content,omitempty"`
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}
type ModelToolChoice struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

type ModelRequest struct {
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	Thinking        *ModelThinking   `json:"thinking,omitempty"`
	Model           string           `json:"model"`
	Messages        []ModelMessage   `json:"messages"`
	Tools           []any            `json:"tools,omitempty"`
	ToolChoice      *ModelToolChoice `json:"tool_choice,omitempty"`
	Stream          bool             `json:"stream"`
	StreamOptions   map[string]any   `json:"stream_options,omitempty"`
	MaxTokens       int              `json:"max_tokens"`
	Temperature     float64          `json:"temperature"`
	EnableThinking  *bool            `json:"enable_thinking,omitempty"`
	ThinkingBudget  int              `json:"thinking_budget,omitempty"`
}

type ModelThinking struct {
	Type string `json:"type"`
}

func applyProviderPolicy(request *ModelRequest) {
	// Choose the official non-thinking tool mode before a new DeepSeek segment
	// starts. Keep explicitly requested thinking mode intact: its complete native
	// reasoning history remains mandatory and is validated before sending.
	// This policy never removes or rewrites messages from an existing segment.
	if strings.HasPrefix(request.Model, "deepseek-") {
		if request.Thinking == nil {
			request.Thinking = &ModelThinking{Type: "disabled"}
		}
		if request.Thinking.Type == "disabled" {
			request.ReasoningEffort = ""
		}
	}
}

type ModelResponse struct {
	Message       ModelMessage
	Usage         Usage
	HasUsage      bool
	StopReason    string
	ReportedModel string
	ResponseID    string
}

func validateEndpoint(ctx context.Context, endpoint string, allowLocal bool) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("模型 URL 格式不正确")
	}
	if allowLocal && (u.Scheme == "http" || u.Scheme == "https") {
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("模型 URL 必须使用 HTTPS")
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", u.Hostname())
	if err != nil || len(ips) == 0 {
		return errors.New("模型域名无法解析")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return errors.New("模型 URL 不能指向内网或本机地址")
		}
	}
	return nil
}
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return false
		}
	}
	return true
}
func modelClient(allowLocal bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 60 * time.Second, MaxIdleConns: 4}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if allowLocal {
			return dialer.DialContext(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("模型域名无法解析")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("模型地址不允许访问")
			}
		}
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, errors.New("模型连接失败")
	}
	return &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// inlineReasoningFilter removes provider reasoning wrappers before content can
// reach SSE subscribers or persistent messages. It buffers only a possible tag
// prefix (or an incomplete tag), so ordinary answer text remains incremental.
type inlineReasoningFilter struct {
	pending string
	stack   []string
	blocked bool
}

func (f *inlineReasoningFilter) Write(chunk string) string {
	if f.blocked {
		return ""
	}
	f.pending += chunk
	var visible strings.Builder
	for f.pending != "" {
		start := strings.IndexByte(f.pending, '<')
		if start < 0 {
			if len(f.stack) == 0 {
				visible.WriteString(f.pending)
			}
			f.pending = ""
			break
		}
		if start > 0 {
			if len(f.stack) == 0 {
				visible.WriteString(f.pending[:start])
			}
			f.pending = f.pending[start:]
		}
		name, closing, selfClosing, size, incomplete := reasoningTag(f.pending)
		if incomplete {
			if len(f.pending) > 4096 {
				f.blocked, f.pending = true, ""
			}
			break
		}
		if size == 0 {
			// A normal less-than sign or unrelated HTML/Markdown tag must not
			// swallow a later reasoning opener in the same chunk.
			if len(f.stack) == 0 {
				visible.WriteByte('<')
			}
			f.pending = f.pending[1:]
			continue
		}
		f.pending = f.pending[size:]
		if closing {
			if len(f.stack) > 0 && f.stack[len(f.stack)-1] == name {
				f.stack = f.stack[:len(f.stack)-1]
			}
			// A mismatched closer cannot reopen output from a malformed block.
		} else if !selfClosing {
			if len(f.stack) >= 64 {
				f.blocked, f.pending = true, ""
				break
			}
			f.stack = append(f.stack, name)
		}
	}
	return visible.String()
}

func (f *inlineReasoningFilter) Finish() string {
	defer func() { f.pending = "" }()
	if f.blocked || len(f.stack) != 0 {
		return ""
	}
	// A bare comparison sign is ordinary text. A partially emitted reasoning
	// name, such as <thi, stays withheld when the provider ends the stream.
	if f.pending == "<" || f.pending == "</" {
		return f.pending
	}
	return ""
}

func reasoningTag(value string) (name string, closing, selfClosing bool, size int, incomplete bool) {
	prefix := strings.ToLower(value[:min(len(value), 12)])
	for _, candidate := range []string{"think", "analysis", "reasoning"} {
		for _, closeTag := range []bool{false, true} {
			head := "<" + candidate
			if closeTag {
				head = "</" + candidate
			}
			if len(value) < len(head) && strings.HasPrefix(head, prefix) {
				return "", false, false, 0, true
			}
			if !strings.HasPrefix(prefix, head) {
				continue
			}
			if len(value) == len(head) {
				return "", false, false, 0, true
			}
			next := value[len(head)]
			if next != '>' && next != '/' && next != ' ' && next != '\t' && next != '\r' && next != '\n' {
				continue
			}
			end := strings.IndexByte(value[len(head):], '>')
			if end < 0 {
				return "", false, false, 0, true
			}
			end += len(head)
			return candidate, closeTag, !closeTag && strings.HasSuffix(strings.TrimSpace(value[len(head):end]), "/"), end + 1, false
		}
	}
	return "", false, false, 0, false
}

const providerDiagnosticLimit = 8 << 10

// providerDiagnosticPrefix retains only a bounded prefix without changing how
// much the parser reads. It is allocated only for explicit private diagnostics.
type providerDiagnosticPrefix struct{ body []byte }

func (p *providerDiagnosticPrefix) Write(data []byte) (int, error) {
	if remaining := providerDiagnosticLimit - len(p.body); remaining > 0 {
		if remaining > len(data) {
			remaining = len(data)
		}
		p.body = append(p.body, data[:remaining]...)
	}
	return len(data), nil
}

func auditProviderFailure(audit func(int, []byte), status int, body []byte) {
	if audit == nil {
		return
	}
	defer func() { _ = recover() }()
	audit(status, append([]byte(nil), body...))
}

func safeModelRequestBody(request ModelRequest, credential string) ([]byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, errors.New("模型请求格式无效")
	}
	if len(body) > modelRequestBytes {
		return nil, errors.New("本轮上下文过大，请缩小分析范围或新建会话")
	}
	if request.Thinking != nil && request.Thinking.Type == "enabled" && len(request.Tools) > 0 {
		for _, m := range request.Messages {
			if m.Role == "assistant" && m.ReasoningContent == nil {
				return nil, errors.New("供应商工具推理协议不完整，分析已停止")
			}
		}
	}
	if credential != "" {
		var payload any
		if json.Unmarshal(body, &payload) != nil {
			return nil, errors.New("模型请求格式无效")
		}
		needle := normalizedSensitiveText(credential)
		var contains func(any, int) bool
		contains = func(value any, depth int) bool {
			switch v := value.(type) {
			case string:
				if needle != "" && strings.Contains(normalizedSensitiveText(v), needle) {
					return true
				}
				// Tool arguments and results can themselves be escaped JSON.
				if depth < 8 {
					var nested any
					if json.Unmarshal([]byte(v), &nested) == nil {
						return contains(nested, depth+1)
					}
				}
			case map[string]any:
				for key, item := range v {
					if contains(key, depth+1) || contains(item, depth+1) {
						return true
					}
				}
			case []any:
				for _, item := range v {
					if contains(item, depth+1) {
						return true
					}
				}
			}
			return false
		}
		if contains(payload, 0) {
			return nil, errors.New("检测到凭证片段，模型请求未发送；请移除该片段后重试")
		}
	}
	return body, nil
}

func callModel(ctx context.Context, cfg Config, allowLocal bool, request ModelRequest, onDelta func(string)) (out ModelResponse, retErr error) {
	out.Message.Role = "assistant"
	applyProviderPolicy(&request)
	body, err := safeModelRequestBody(request, cfg.APIKey)
	if err != nil {
		return out, err
	}
	if err := validateEndpoint(ctx, cfg.BaseURL, allowLocal); err != nil {
		return out, err
	}
	if cfg.RequestAudit != nil {
		if err := cfg.RequestAudit(append([]byte(nil), body...)); err != nil {
			return out, errors.New("本地出站数据核验未通过，模型请求未发送")
		}
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	var wroteRequest atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) { wroteRequest.Store(info.Err == nil) },
	}))
	client := modelClient(allowLocal)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		phase := "connect"
		if wroteRequest.Load() {
			phase = "response_headers"
		}
		return out, &providerFailure{kind: "transport", phase: phase, reason: providerTransportReason(ctx, err), message: "模型连接失败或请求已中断"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, providerDiagnosticLimit))
		if cfg.ProviderErrorAudit != nil {
			// Optional isolated diagnostics only. Do not retain/return the raw
			// body, infer usage from an error, or expose response headers.
			auditProviderFailure(cfg.ProviderErrorAudit, resp.StatusCode, raw)
		}
		if providerContextError(raw) {
			return out, &providerFailure{status: resp.StatusCode, kind: "context", message: "供应商拒绝上下文容量，已保留成果，请缩小分析范围"}
		}
		return out, &providerFailure{status: resp.StatusCode, kind: "http", phase: "response_status", reason: "non_success", message: fmt.Sprintf("模型服务返回 HTTP %d，请检查配置、余额或稍后重试", resp.StatusCode)}
	}
	// Every error after an accepted response has a known HTTP status. Retain
	// that fact even for malformed JSON, invalid usage or an in-stream error;
	// only fixed local messages and allowlisted categories leave this client.
	defer func() {
		if retErr == nil {
			return
		}
		var failure *providerFailure
		if !errors.As(retErr, &failure) {
			retErr = &providerFailure{status: resp.StatusCode, kind: "protocol", phase: "response_body", reason: "invalid_response", message: retErr.Error()}
			return
		}
		failure.status = resp.StatusCode
		if failure.phase == "" {
			failure.phase = "response_body"
		}
	}()
	type wire struct {
		Model   string `json:"model"`
		ID      string `json:"id"`
		Choices []struct {
			FinishReason string       `json:"finish_reason"`
			Message      ModelMessage `json:"message"`
			Delta        struct {
				ReasoningContent *string `json:"reasoning_content"`
				Content          string  `json:"content"`
				ToolCalls        []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
		Error json.RawMessage `json:"error"`
	}
	var reasoning inlineReasoningFilter
	credentials := newCredentialStreamFilter(cfg.APIKey)
	appendVisible := func(content string) {
		if content == "" {
			return
		}
		out.Message.Content += content
		if request.Stream && onDelta != nil {
			onDelta(content)
		}
	}
	emitVisible := func(content string) { appendVisible(credentials.Write(content)) }
	finishContent := func() error {
		emitVisible(reasoning.Finish())
		appendVisible(credentials.Finish())
		if strings.TrimSpace(out.Message.Content) == "" && len(out.Message.ToolCalls) == 0 {
			return errors.New("模型未返回可展示的回答，请重试或调整模型配置")
		}
		return nil
	}
	consume := func(w wire) error {
		if len(w.Error) > 0 && string(w.Error) != "null" {
			if providerContextError([]byte(`{"error":` + string(w.Error) + `}`)) {
				return &providerFailure{kind: "context", message: "供应商拒绝上下文容量，已保留成果，请缩小分析范围"}
			}
			return &providerFailure{kind: "upstream", message: "模型服务在生成过程中返回错误"}
		}
		// These identifiers are supplier assertions, not proof of an upstream
		// model version. Keep unknown values empty and never persist a key echo.
		if value := safeProviderIdentifier(w.Model, cfg.APIKey); value != "" {
			out.ReportedModel = value
		}
		if value := safeProviderIdentifier(w.ID, cfg.APIKey); value != "" {
			out.ResponseID = value
		}
		if w.Usage != nil {
			if w.Usage.Input < 0 || w.Usage.Output < 0 || w.Usage.Input > 2_000_000 || w.Usage.Output > 2_000_000 {
				return errors.New("模型返回无效用量，已保留预留费用供核对")
			}
			out.Usage = Usage{InputTokens: w.Usage.Input, OutputTokens: w.Usage.Output}
			out.HasUsage = true
		}
		if len(w.Choices) == 0 {
			return nil
		}
		choice := w.Choices[0]
		if choice.FinishReason != "" {
			out.StopReason = choice.FinishReason
		}
		if !request.Stream {
			out.Message = choice.Message
			if request.Thinking == nil || request.Thinking.Type != "enabled" {
				out.Message.ReasoningContent = nil
			}
			out.Message.Content = ""
			emitVisible(reasoning.Write(choice.Message.Content))
			return nil
		}
		if request.Thinking != nil && request.Thinking.Type == "enabled" && choice.Delta.ReasoningContent != nil {
			if out.Message.ReasoningContent == nil {
				value := ""
				out.Message.ReasoningContent = &value
			}
			if len(*out.Message.ReasoningContent)+len(*choice.Delta.ReasoningContent) > 65536 {
				return errors.New("模型推理上下文达到容量上限，请缩小范围")
			}
			*out.Message.ReasoningContent += *choice.Delta.ReasoningContent
		}
		if choice.Delta.Content != "" {
			emitVisible(reasoning.Write(choice.Delta.Content))
		}
		for _, part := range choice.Delta.ToolCalls {
			if part.Index < 0 || part.Index > 31 {
				return errors.New("模型工具调用格式无效")
			}
			for len(out.Message.ToolCalls) <= part.Index {
				out.Message.ToolCalls = append(out.Message.ToolCalls, ToolCall{Type: "function"})
			}
			call := &out.Message.ToolCalls[part.Index]
			call.ID += part.ID
			call.Function.Name += part.Function.Name
			call.Function.Arguments += part.Function.Arguments
			if len(call.Function.Arguments) > 65536 {
				return errors.New("模型工具参数过大")
			}
		}
		return nil
	}
	if !request.Stream {
		var w wire
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&w); err != nil {
			return out, errors.New("模型响应格式无效")
		}
		if err := consume(w); err != nil {
			return out, err
		}
		if err := finishContent(); err != nil {
			return out, err
		}
		if out.StopReason == "length" {
			return out, errors.New("回答达到模型输出上限，已生成内容已保留，请缩小问题或继续")
		}
		return out, nil
	}
	var streamBody io.Reader = resp.Body
	var diagnostic *providerDiagnosticPrefix
	if cfg.ProviderErrorAudit != nil {
		diagnostic = &providerDiagnosticPrefix{}
		streamBody = io.TeeReader(streamBody, diagnostic)
	}
	auditStreamFailure := func() {
		if diagnostic != nil {
			auditProviderFailure(cfg.ProviderErrorAudit, resp.StatusCode, diagnostic.body)
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(streamBody, 4<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var w wire
		if json.Unmarshal([]byte(data), &w) != nil {
			auditStreamFailure()
			return out, errors.New("模型流式响应格式无效")
		}
		if err := consume(w); err != nil {
			auditStreamFailure()
			return out, err
		}
	}
	if scanner.Err() != nil || !done {
		auditStreamFailure()
		reason := "truncated"
		if scanner.Err() != nil {
			reason = providerTransportReason(ctx, scanner.Err())
		}
		return out, &providerFailure{status: resp.StatusCode, kind: "transport", phase: "response_body", reason: reason, message: "模型响应中断，请重试"}
	}
	if err := finishContent(); err != nil {
		return out, err
	}
	if out.StopReason == "length" {
		return out, errors.New("回答达到模型输出上限，已生成内容已保留，请缩小问题或继续")
	}
	return out, nil
}

// Persist only this allowlist, never err.Error(): transport errors may include
// endpoint URLs, proxy credentials or arbitrary upstream text.
func providerTransportReason(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var timed net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &timed) && timed.Timeout()) {
		return "timeout"
	}
	return "connection"
}

func safeProviderIdentifier(value, apiKey string) string {
	if value == "" || len(value) > 256 || (apiKey != "" && strings.Contains(value, apiKey)) {
		return ""
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.:/", r)) {
			return ""
		}
	}
	return value
}

func tool(name, description string, properties map[string]any, required ...string) any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}}
}
func agentTools() []any {
	str := func(description string) any { return map[string]any{"type": "string", "description": description} }
	integer := map[string]any{"type": "integer"}
	return []any{
		tool("describe_data", "默认读取简明目录与必备规则；tables读取指定表的详细列/枚举/关联/含义，topic=metrics或rules。has_more时用next_offset读下一页。", map[string]any{"tables": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "topic": str("metrics或rules；留空读目录"), "offset": integer}),
		tool("query_data", "对本轮脱敏快照执行单条只读 SELECT/WITH SQL。使用describe_data的枚举、指标与关联键，不猜值；聚合覆盖全部匹配记录。金额按cents计算，展示人民币须在SQL中除100并命名单位。需要展示子集时重新查询实际筛选后的结果，不沿用探查全量result_id。需要原记录下钻时显式返回record_type（原业务表名）、record_id（原主键）、lead_id；analysis_records已有这些列，source_table/source_id等自定义别名不会自动成为受控来源卡片。", map[string]any{"sql": str("按字典定义的只读SQL，禁止写入和系统表"), "limit": integer}, "sql"),
		tool("search_records", "检索业务记录文本，分页并给出总数和来源。", map[string]any{"keyword": str("关键词"), "lead_id": integer, "actor_id": str("人员业务字符串标识，对应crm_wecom_user_bindings.wecom_userid，不是数字账号ID"), "from": str("开始时间含下界；精确时间需时区，YYYY-MM-DD按上海当日00:00"), "to": str("结束时间包含上界；带时区的精确时间包含该时刻，YYYY-MM-DD仅日期按上海当日23:59:59.999999999截止；不是半开区间，不要以次日00:00代替当日结束"), "stage": str("存储枚举invitation/negotiation/finished；留空不限阶段"), "offset": integer, "limit": integer}),
		tool("measure_record_set", "服务端全量计算原始记录集合的全局和分组去重，不能相加分组线索数。source指定未筛选业务原表，或sql选择集合并返回原表名record_type及整数record_id；只能选其一。未知记录报错。返回事实fact_id及global/group层级。", map[string]any{"source": str("允许的原业务表名，无筛选时使用"), "sql": str("单条只读SQL，返回record_type与record_id，_history记录仍用原表名record_type"), "visibility": str("current默认排除已删除线索，history含历史"), "group_by": str("actor_user_id/record_type/business_stage/occurred_date_shanghai/source_channel；留空只算全局")}),
		tool("read_result", "读取本轮已保存结果的页或长文本分段，不重查业务库。offset按结果行；单列文本用row、column、text_offset。页或text_segments标部分时不能称读完。", map[string]any{"result_id": str("本轮真实结果ID"), "offset": integer, "limit": integer, "row": integer, "column": str("文本列"), "text_offset": integer}, "result_id"),
		tool("read_history", "读取当前会话指定用户/已完成回答的历史文字分段。仅用于恢复意图与指代，旧答案不是当前事实。", map[string]any{"message_id": str("本会话真实消息ID"), "offset": integer}, "message_id"),
		tool("select_final_results", "回答前明确最终采用的结果，排除废弃探索；纠错或继续查询后重新选择。仅文字结论也须选择依据，未采用数据用[]，禁止省略或null。", map[string]any{"result_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "result_ids"),
		tool("finalize_answer", "提交最终答案并立即完成任务，必须是最后一个工具。可在本工具result_ids声明最终依据，或先select_final_results。markdown默认只写解释，不写占位符；references引用fact_id+level(global/group)，或result_id+row(0起)+column+level(cell)，服务端自动列完整数据陈述、数值与行标签，通常省略token。统计数字、范围、数量不能自填。事实或单元格必须实际读取且最终采用。", map[string]any{"result_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "table_result_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Display these selected result tables in this same final submission; no separate present_result call is needed."}, "markdown": str("中文安全Markdown解释，不手写数字或表格；默认省略占位符，让references自动展示"), "references": map[string]any{"type": "array", "description": `默认省略token与正文占位符；服务端自动列出引用及同行员工名称，无需重复引用名称列。有效JSON结构示例：{"markdown":"已按实际操作人核对。","references":[{"result_id":"工具返回的真实ID","row":0,"column":"工具返回的实际列名","level":"cell"}]}。示例ID/列名必须替换为已读取真实值。只有显式排版时才用唯一token与独立段落{{token}}，不能在段内附指标/实体/单位；一个token只对应一个引用。SQL列名是查询指标，不证明业务语义。`, "items": map[string]any{"type": "object", "properties": map[string]any{"token": map[string]any{"type": "string", "description": "可省略。仅显式正文占位符使用；每个引用必须唯一，字母开头，仅字母数字下划线，最多40字符。绝不把同一token同时绑定数值和姓名。", "pattern": "^[a-zA-Z][a-zA-Z0-9_]{0,39}$", "minLength": 1, "maxLength": 40}, "fact_id": str("measure_record_set的真实事实ID"), "result_id": str("单元格真实结果ID"), "row": integer, "column": str("单元格列名"), "level": str("global/group/cell"), "comparison": str("可选snapshot：仅已读时间单元格，服务器比较与本轮快照先后；日期按上海日期，未知或无时区值不能比较")}, "required": []string{}, "additionalProperties": false}}, "conditions": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"kind": str("date/actor/source/status/other"), "text": str("用户原文条件片段，不自行推算"), "source_message_id": str("条件来源真实用户消息ID"), "remove": map[string]any{"type": "boolean"}}, "required": []string{"kind", "text", "source_message_id"}, "additionalProperties": false}}}, "markdown", "references"),
		tool("present_result", "展示本轮真实result_id绑定的全部行，不执行筛选或单位换算。标题、人员/日期/状态人群和单位必须与全部行一致；不一致须先重新query_data取得正确子集，不能只改标题或文字。", map[string]any{"type": str("table或chart"), "title": str("与结果全部行、筛选范围和数值单位一致的标题"), "result_id": str("本轮已获得且范围与标题一致的结果ID"), "chart_type": str("bar,line,pie,scatter"), "x_column": str("真实横轴列"), "y_columns": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "type", "result_id"),
	}
}

// Prices are integer micro-yuan per million tokens. Round reservations upward.
func modelCost(input, output int64, cfg Config) int64 {
	return (input*cfg.InputPriceMicro+999999)/1000000 + (output*cfg.OutputPriceMicro+999999)/1000000
}
