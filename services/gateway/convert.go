package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	toolReasoningPlaceholder             = "tool call"
	anthropicRedactedThinkingPlaceholder = "[redacted thinking]"
)

var reasoningVendorHints = [...]string{"moonshot", "kimi", "deepseek", "mimo", "xiaomimimo"}

type bridgeBlock struct {
	Kind          string
	Text          string
	Signature     string
	Encrypted     string
	URL           string
	MediaType     string
	Data          string
	ID            string
	Name          string
	Arguments     any
	ArgumentsJSON string
	CallID        string
	Result        any
	IsError       bool
}

type bridgeMessage struct {
	Role   string
	Blocks []bridgeBlock
}

type bridgeTool struct {
	Name        string
	Description string
	Schema      any
	Strict      bool
}

type bridgeToolChoice struct {
	Mode string
	Name string
}

type bridgeRequest struct {
	Model       string
	System      []bridgeBlock
	Messages    []bridgeMessage
	Tools       []bridgeTool
	ToolChoice  bridgeToolChoice
	Stream      bool
	Temperature any
	TopP        any
	MaxTokens   any
	Stop        any
	Reasoning   any
	Metadata    any
}

type bridgeUsage struct {
	Input     int
	Output    int
	Total     int
	Cached    int
	Reasoning int
}

type bridgeResponse struct {
	ID        string
	Model     string
	Text      string
	Reasoning []bridgeBlock
	Tools     []bridgeBlock
	Stop      string
	Usage     bridgeUsage
	Created   int64
}

func convertRequest(from, to Protocol, input map[string]any) (map[string]any, error) {
	if from == to {
		return cloneMap(input), nil
	}
	request, err := decodeBridgeRequest(from, input)
	if err != nil {
		return nil, err
	}
	return encodeBridgeRequest(to, request)
}

// prepareUpstreamRequest is the single request preparation path for both
// pass-through and transcoded requests. Same-protocol requests are cloned so
// provider-specific fields survive, while cross-protocol requests go through
// the bridge. Target-protocol normalization then repairs reasoning history in
// either case.
func prepareUpstreamRequest(from, to Protocol, input map[string]any, upstreamURL string) (map[string]any, error) {
	output, err := convertRequest(from, to, input)
	if err != nil {
		return nil, err
	}
	normalizeToolReasoningHistory(to, stringAt(output, "model"), upstreamURL, output)
	return output, nil
}

// normalizeToolReasoningHistory applies only to endpoints that are known to
// require reasoning replay, or to requests that explicitly enable reasoning.
// Normalizing the target shape makes the behavior independent of the client
// protocol used to reach the gateway.
func normalizeToolReasoningHistory(protocol Protocol, model, upstreamURL string, input map[string]any) bool {
	if !shouldNormalizeToolReasoningHistory(model, upstreamURL, input) {
		return false
	}
	switch protocol {
	case ProtocolChat:
		return normalizeChatToolReasoningHistory(input)
	case ProtocolAnthropic:
		return normalizeAnthropicToolThinkingHistory(input)
	default:
		return false
	}
}

// shouldNormalizeToolReasoningHistory identifies providers whose compatible
// endpoints require thinking/reasoning to be replayed with assistant tool
// calls. Explicit reasoning settings also cover aliased model names.
func shouldNormalizeToolReasoningHistory(model, upstreamURL string, input map[string]any) bool {
	return isReasoningVendorIdentifier(model) || isReasoningVendorIdentifier(upstreamURL) || requestEnablesReasoning(input)
}

func isReasoningVendorIdentifier(value string) bool {
	value = strings.ToLower(value)
	for _, hint := range reasoningVendorHints {
		if strings.Contains(value, hint) {
			return true
		}
	}
	return false
}

func requestEnablesReasoning(input map[string]any) bool {
	for _, key := range []string{"reasoning_effort", "reasoning", "thinking", "effort"} {
		value, exists := input[key]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			mode := strings.ToLower(strings.TrimSpace(typed))
			if mode != "" && mode != "none" && mode != "disabled" {
				return true
			}
		case bool:
			if typed {
				return true
			}
		case map[string]any:
			mode := strings.ToLower(strings.TrimSpace(firstString(stringAt(typed, "type"), stringAt(typed, "effort"))))
			if mode == "none" || mode == "disabled" {
				continue
			}
			return true
		default:
			return true
		}
	}
	return false
}

// normalizeChatToolReasoningHistory ensures every assistant tool-call turn
// carries reasoning_content. Some clients discard this non-standard field
// while retaining tool_calls, which otherwise makes the next thinking-mode
// request invalid. A legacy reasoning string is promoted when available.
func normalizeChatToolReasoningHistory(input map[string]any) bool {
	messages, ok := input["messages"].([]any)
	if !ok {
		return false
	}

	changed := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok || stringAt(message, "role") != "assistant" || len(sliceAt(message, "tool_calls")) == 0 {
			continue
		}
		if reasoning, ok := message["reasoning_content"].(string); ok && strings.TrimSpace(reasoning) != "" {
			continue
		}
		reasoning, _ := message["reasoning"].(string)
		if strings.TrimSpace(reasoning) == "" {
			reasoning = toolReasoningPlaceholder
		}
		message["reasoning_content"] = reasoning
		changed = true
	}
	return changed
}

// normalizeAnthropicToolThinkingHistory repairs only assistant turns that
// contain tool_use. DeepSeek, Kimi/Moonshot and MiMo reject signed, redacted,
// empty or missing thinking history on those turns even though Anthropic
// clients can legitimately send each of those shapes.
func normalizeAnthropicToolThinkingHistory(input map[string]any) bool {
	messages, ok := input["messages"].([]any)
	if !ok {
		return false
	}

	changed := false
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok || stringAt(message, "role") != "assistant" {
			continue
		}
		content, ok := message["content"].([]any)
		if !ok || !anthropicContentHasType(content, "tool_use") {
			continue
		}

		hasThinking := false
		for i, rawBlock := range content {
			block, ok := rawBlock.(map[string]any)
			if !ok {
				continue
			}
			switch stringAt(block, "type") {
			case "thinking":
				hasThinking = true
				if _, exists := block["signature"]; exists {
					delete(block, "signature")
					changed = true
				}
				thinking, ok := block["thinking"].(string)
				if !ok || strings.TrimSpace(thinking) == "" {
					block["thinking"] = toolReasoningPlaceholder
					changed = true
				}
			case "redacted_thinking":
				hasThinking = true
				content[i] = map[string]any{
					"type":     "thinking",
					"thinking": anthropicRedactedThinkingPlaceholder,
				}
				changed = true
			}
		}
		if !hasThinking {
			content = append([]any{map[string]any{
				"type":     "thinking",
				"thinking": toolReasoningPlaceholder,
			}}, content...)
			message["content"] = content
			changed = true
		}
	}
	return changed
}

func anthropicContentHasType(content []any, kind string) bool {
	for _, rawBlock := range content {
		block, _ := rawBlock.(map[string]any)
		if stringAt(block, "type") == kind {
			return true
		}
	}
	return false
}

func decodeBridgeRequest(protocol Protocol, input map[string]any) (bridgeRequest, error) {
	request := bridgeRequest{
		Model:       stringAt(input, "model"),
		Stream:      boolAt(input, "stream"),
		Temperature: input["temperature"],
		TopP:        input["top_p"],
		Metadata:    input["metadata"],
	}
	switch protocol {
	case ProtocolChat:
		request.MaxTokens = firstAny(input["max_completion_tokens"], input["max_tokens"])
		request.Stop = input["stop"]
		request.Reasoning = firstAny(input["reasoning_effort"], input["reasoning"])
		for i, raw := range sliceAt(input, "messages") {
			message, ok := raw.(map[string]any)
			if !ok {
				return request, fmt.Errorf("messages[%d] must be an object", i)
			}
			role := stringAt(message, "role")
			blocks := decodeOpenAIBlocks(message["content"])
			if role == "assistant" {
				blocks = append(decodeChatReasoning(message), blocks...)
			}
			for j, rawCall := range sliceAt(message, "tool_calls") {
				call, ok := rawCall.(map[string]any)
				if !ok {
					return request, fmt.Errorf("messages[%d].tool_calls[%d] must be an object", i, j)
				}
				function := mapAt(call, "function")
				blocks = append(blocks, bridgeBlock{
					Kind:          "tool_call",
					ID:            stringAt(call, "id"),
					Name:          stringAt(function, "name"),
					ArgumentsJSON: stringAt(function, "arguments"),
				})
			}
			switch role {
			case "system", "developer":
				request.System = append(request.System, blocks...)
			case "tool":
				request.Messages = append(request.Messages, bridgeMessage{Role: "user", Blocks: []bridgeBlock{{
					Kind:   "tool_result",
					CallID: stringAt(message, "tool_call_id"),
					Result: message["content"],
				}}})
			case "user", "assistant":
				request.Messages = append(request.Messages, bridgeMessage{Role: role, Blocks: blocks})
			default:
				return request, fmt.Errorf("messages[%d] has unsupported role %q", i, role)
			}
		}
		for i, raw := range sliceAt(input, "tools") {
			tool, ok := raw.(map[string]any)
			if !ok || stringAt(tool, "type") != "function" {
				return request, fmt.Errorf("tools[%d] must be a function tool", i)
			}
			function := mapAt(tool, "function")
			request.Tools = append(request.Tools, bridgeTool{
				Name:        stringAt(function, "name"),
				Description: stringAt(function, "description"),
				Schema:      function["parameters"],
				Strict:      boolAt(function, "strict"),
			})
		}
		request.ToolChoice = decodeChatToolChoice(input["tool_choice"])

	case ProtocolResponses:
		request.MaxTokens = input["max_output_tokens"]
		request.Stop = input["stop"]
		request.Reasoning = input["reasoning"]
		request.System = append(request.System, decodeOpenAIBlocks(input["instructions"])...)
		switch value := input["input"].(type) {
		case string:
			request.Messages = append(request.Messages, bridgeMessage{Role: "user", Blocks: []bridgeBlock{{Kind: "text", Text: value}}})
		case []any:
			for i, raw := range value {
				item, ok := raw.(map[string]any)
				if !ok {
					return request, fmt.Errorf("input[%d] must be an object", i)
				}
				switch stringAt(item, "type") {
				case "reasoning":
					for _, block := range decodeResponsesReasoning(item) {
						appendBridgeBlock(&request.Messages, "assistant", block)
					}
				case "function_call":
					appendBridgeBlock(&request.Messages, "assistant", bridgeBlock{
						Kind:          "tool_call",
						ID:            firstString(stringAt(item, "call_id"), stringAt(item, "id")),
						Name:          stringAt(item, "name"),
						ArgumentsJSON: stringAt(item, "arguments"),
					})
				case "function_call_output":
					appendBridgeBlock(&request.Messages, "user", bridgeBlock{
						Kind:   "tool_result",
						CallID: stringAt(item, "call_id"),
						Result: item["output"],
					})
				case "message", "":
					role := stringAt(item, "role")
					blocks := decodeOpenAIBlocks(item["content"])
					if role == "system" || role == "developer" {
						request.System = append(request.System, blocks...)
					} else if role == "user" || role == "assistant" {
						request.Messages = append(request.Messages, bridgeMessage{Role: role, Blocks: blocks})
					}
				}
			}
		default:
			return request, fmt.Errorf("input must be a string or array")
		}
		for i, raw := range sliceAt(input, "tools") {
			tool, ok := raw.(map[string]any)
			if !ok || stringAt(tool, "type") != "function" {
				return request, fmt.Errorf("tools[%d] must be a function tool", i)
			}
			request.Tools = append(request.Tools, bridgeTool{
				Name:        stringAt(tool, "name"),
				Description: stringAt(tool, "description"),
				Schema:      tool["parameters"],
				Strict:      boolAt(tool, "strict"),
			})
		}
		request.ToolChoice = decodeResponsesToolChoice(input["tool_choice"])

	case ProtocolAnthropic:
		request.MaxTokens = input["max_tokens"]
		request.Stop = input["stop_sequences"]
		request.Reasoning = firstAny(input["thinking"], input["effort"])
		request.System = decodeAnthropicBlocks(input["system"])
		for i, raw := range sliceAt(input, "messages") {
			message, ok := raw.(map[string]any)
			if !ok {
				return request, fmt.Errorf("messages[%d] must be an object", i)
			}
			role := stringAt(message, "role")
			if role != "user" && role != "assistant" {
				return request, fmt.Errorf("messages[%d] has unsupported role %q", i, role)
			}
			request.Messages = append(request.Messages, bridgeMessage{Role: role, Blocks: decodeAnthropicBlocks(message["content"])})
		}
		for i, raw := range sliceAt(input, "tools") {
			tool, ok := raw.(map[string]any)
			if !ok {
				return request, fmt.Errorf("tools[%d] must be an object", i)
			}
			request.Tools = append(request.Tools, bridgeTool{
				Name:        stringAt(tool, "name"),
				Description: stringAt(tool, "description"),
				Schema:      tool["input_schema"],
			})
		}
		request.ToolChoice = decodeAnthropicToolChoice(input["tool_choice"])

	default:
		return request, fmt.Errorf("unsupported input protocol %q", protocol)
	}
	return request, nil
}

func appendBridgeBlock(messages *[]bridgeMessage, role string, block bridgeBlock) {
	if len(*messages) > 0 {
		last := &(*messages)[len(*messages)-1]
		if last.Role == role && bridgeBlocksOnlyTools(last.Blocks) {
			last.Blocks = append(last.Blocks, block)
			return
		}
	}
	*messages = append(*messages, bridgeMessage{Role: role, Blocks: []bridgeBlock{block}})
}

func bridgeBlocksOnlyTools(blocks []bridgeBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, block := range blocks {
		if block.Kind != "tool_call" && block.Kind != "tool_result" {
			return false
		}
	}
	return true
}

func encodeBridgeRequest(protocol Protocol, request bridgeRequest) (map[string]any, error) {
	switch protocol {
	case ProtocolChat:
		return encodeChatRequest(request)
	case ProtocolResponses:
		return encodeResponsesRequest(request), nil
	case ProtocolAnthropic:
		return encodeAnthropicRequest(request), nil
	default:
		return nil, fmt.Errorf("unsupported output protocol %q", protocol)
	}
}

func encodeChatRequest(request bridgeRequest) (map[string]any, error) {
	output := map[string]any{"model": request.Model, "stream": request.Stream}
	put(output, "temperature", request.Temperature)
	put(output, "top_p", request.TopP)
	put(output, "max_tokens", request.MaxTokens)
	put(output, "stop", request.Stop)
	if effort := reasoningEffort(request.Reasoning); effort != nil {
		output["reasoning_effort"] = effort
	}
	if request.Stream {
		output["stream_options"] = map[string]any{"include_usage": true}
	}

	messages := make([]any, 0, len(request.Messages)+1)
	if len(request.System) > 0 {
		messages = append(messages, map[string]any{"role": "system", "content": encodeChatBlocks(request.System)})
	}
	pending := make(map[string]bool)
	pendingResults := make(map[string]bridgeBlock)
	var pendingOrder []string
	var deferred [][]bridgeBlock
	flushDeferred := func() {
		for _, blocks := range deferred {
			if len(blocks) > 0 {
				messages = append(messages, map[string]any{"role": "user", "content": encodeChatBlocks(blocks)})
			}
		}
		deferred = nil
	}

	for i, message := range request.Messages {
		var content, reasoning, calls, results []bridgeBlock
		for _, block := range message.Blocks {
			switch block.Kind {
			case "reasoning":
				reasoning = append(reasoning, block)
			case "tool_call":
				calls = append(calls, block)
			case "tool_result":
				results = append(results, block)
			default:
				content = append(content, block)
			}
		}
		if message.Role == "assistant" {
			if len(pending) > 0 {
				return nil, fmt.Errorf("messages[%d]: assistant message appears before tool results for %s", i, strings.Join(missingToolIDs(pendingOrder, pending), ", "))
			}
			if len(results) > 0 {
				return nil, fmt.Errorf("messages[%d]: assistant message contains tool results", i)
			}
			encoded := map[string]any{"role": "assistant", "content": nil}
			if value, ok := bridgeReasoningText(reasoning); ok {
				encoded["reasoning_content"] = value
			}
			if len(content) > 0 {
				encoded["content"] = encodeChatBlocks(content)
			}
			if len(calls) > 0 {
				toolCalls := make([]any, 0, len(calls))
				for _, call := range calls {
					if call.ID == "" || call.Name == "" {
						return nil, fmt.Errorf("messages[%d]: tool call must contain id and name", i)
					}
					if pending[call.ID] {
						return nil, fmt.Errorf("messages[%d]: duplicate tool call id %q", i, call.ID)
					}
					pending[call.ID] = true
					pendingOrder = append(pendingOrder, call.ID)
					toolCalls = append(toolCalls, map[string]any{
						"id":   call.ID,
						"type": "function",
						"function": map[string]any{
							"name":      call.Name,
							"arguments": bridgeArgumentsJSON(call),
						},
					})
				}
				encoded["tool_calls"] = toolCalls
			}
			if len(content) > 0 || len(reasoning) > 0 || len(calls) > 0 {
				messages = append(messages, encoded)
			}
			continue
		}

		if len(calls) > 0 {
			return nil, fmt.Errorf("messages[%d]: user message contains tool calls", i)
		}
		for _, result := range results {
			if _, duplicate := pendingResults[result.CallID]; duplicate {
				return nil, fmt.Errorf("messages[%d]: duplicate tool result %q", i, result.CallID)
			}
			if !pending[result.CallID] {
				return nil, fmt.Errorf("messages[%d]: tool result %q has no pending tool call", i, result.CallID)
			}
			pendingResults[result.CallID] = result
			delete(pending, result.CallID)
		}
		if len(content) > 0 {
			if len(pendingOrder) > 0 || len(deferred) > 0 {
				deferred = append(deferred, content)
			} else {
				messages = append(messages, map[string]any{"role": "user", "content": encodeChatBlocks(content)})
			}
		}
		if len(pending) == 0 && len(pendingOrder) > 0 {
			for _, id := range pendingOrder {
				result := pendingResults[id]
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": id,
					"content":      bridgeToolResultContent(result.Result),
				})
			}
			flushDeferred()
			pendingOrder = nil
			clear(pendingResults)
		}
	}
	if len(pending) > 0 {
		return nil, fmt.Errorf("tool calls are missing results for %s", strings.Join(missingToolIDs(pendingOrder, pending), ", "))
	}
	flushDeferred()
	output["messages"] = messages

	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			function := map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  schemaOrDefault(tool.Schema),
			}
			if tool.Strict {
				function["strict"] = true
			}
			tools = append(tools, map[string]any{"type": "function", "function": function})
		}
		output["tools"] = tools
		if choice := encodeChatToolChoice(request.ToolChoice); choice != nil {
			output["tool_choice"] = choice
		}
	}
	return output, nil
}

func missingToolIDs(order []string, pending map[string]bool) []string {
	missing := make([]string, 0, len(pending))
	for _, id := range order {
		if pending[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == len(pending) {
		return missing
	}
	for id := range pending {
		found := false
		for _, current := range missing {
			if current == id {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

func encodeResponsesRequest(request bridgeRequest) map[string]any {
	output := map[string]any{"model": request.Model, "stream": request.Stream}
	put(output, "temperature", request.Temperature)
	put(output, "top_p", request.TopP)
	put(output, "max_output_tokens", request.MaxTokens)
	put(output, "stop", request.Stop)
	put(output, "metadata", request.Metadata)
	if len(request.System) > 0 {
		output["instructions"] = bridgeBlocksText(request.System)
	}
	if request.Reasoning != nil {
		switch value := request.Reasoning.(type) {
		case string:
			output["reasoning"] = map[string]any{"effort": value}
		default:
			output["reasoning"] = value
		}
	}

	items := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		var content []any
		flushContent := func() {
			if len(content) == 0 {
				return
			}
			items = append(items, map[string]any{
				"type":    "message",
				"role":    normalizeResponsesRole(message.Role),
				"content": content,
			})
			content = nil
		}
		for _, block := range message.Blocks {
			switch block.Kind {
			case "reasoning":
				flushContent()
				items = append(items, encodeResponsesReasoning(block, false))
			case "text":
				kind := "input_text"
				if message.Role == "assistant" {
					kind = "output_text"
				}
				content = append(content, map[string]any{"type": kind, "text": block.Text})
			case "image":
				url := block.URL
				if url == "" && block.Data != "" {
					url = "data:" + block.MediaType + ";base64," + block.Data
				}
				content = append(content, map[string]any{"type": "input_image", "image_url": url})
			case "tool_call":
				flushContent()
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   block.ID,
					"name":      block.Name,
					"arguments": bridgeArgumentsJSON(block),
				})
			case "tool_result":
				flushContent()
				items = append(items, map[string]any{
					"type":    "function_call_output",
					"call_id": block.CallID,
					"output":  bridgeToolResultContent(block.Result),
				})
			}
		}
		flushContent()
	}
	output["input"] = items
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  schemaOrDefault(tool.Schema),
				"strict":      tool.Strict,
			})
		}
		output["tools"] = tools
		if choice := encodeResponsesToolChoice(request.ToolChoice); choice != nil {
			output["tool_choice"] = choice
		}
	}
	return output
}

func encodeAnthropicRequest(request bridgeRequest) map[string]any {
	output := map[string]any{"model": request.Model, "stream": request.Stream}
	put(output, "temperature", request.Temperature)
	put(output, "top_p", request.TopP)
	put(output, "stop_sequences", request.Stop)
	if request.MaxTokens == nil {
		output["max_tokens"] = 4096
	} else {
		output["max_tokens"] = request.MaxTokens
	}
	if effort := reasoningEffort(request.Reasoning); effort != nil {
		output["effort"] = effort
	}
	if len(request.System) > 0 {
		output["system"] = encodeAnthropicBlocks(request.System)
	}

	messages := make([]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		content := encodeAnthropicBlocks(message.Blocks)
		if len(content) == 0 {
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "assistant"
		}
		if len(messages) > 0 {
			last, _ := messages[len(messages)-1].(map[string]any)
			if stringAt(last, "role") == role {
				last["content"] = append(asSlice(last["content"]), content...)
				continue
			}
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}
	output["messages"] = messages
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": schemaOrDefault(tool.Schema),
			})
		}
		output["tools"] = tools
		if choice := encodeAnthropicToolChoice(request.ToolChoice); choice != nil {
			output["tool_choice"] = choice
		}
	}
	return output
}

func decodeOpenAIBlocks(value any) []bridgeBlock {
	switch value := value.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []bridgeBlock{{Kind: "text", Text: value}}
	case map[string]any:
		return decodeOpenAIBlocks([]any{value})
	case []any:
		blocks := make([]bridgeBlock, 0, len(value))
		for _, raw := range value {
			part, _ := raw.(map[string]any)
			switch stringAt(part, "type") {
			case "text", "input_text", "output_text":
				blocks = append(blocks, bridgeBlock{Kind: "text", Text: stringAt(part, "text")})
			case "image_url":
				blocks = append(blocks, bridgeBlock{Kind: "image", URL: firstString(stringAt(part, "image_url", "url"), stringAt(part, "image_url"))})
			case "input_image":
				blocks = append(blocks, bridgeBlock{Kind: "image", URL: stringAt(part, "image_url")})
			}
		}
		return blocks
	default:
		return nil
	}
}

func decodeAnthropicBlocks(value any) []bridgeBlock {
	if text, ok := value.(string); ok {
		if text == "" {
			return nil
		}
		return []bridgeBlock{{Kind: "text", Text: text}}
	}
	var blocks []bridgeBlock
	for _, raw := range asSlice(value) {
		part, _ := raw.(map[string]any)
		switch stringAt(part, "type") {
		case "thinking":
			blocks = append(blocks, bridgeBlock{
				Kind:      "reasoning",
				Text:      stringAt(part, "thinking"),
				Signature: stringAt(part, "signature"),
			})
		case "redacted_thinking":
			blocks = append(blocks, bridgeBlock{Kind: "reasoning", Encrypted: stringAt(part, "data")})
		case "text":
			blocks = append(blocks, bridgeBlock{Kind: "text", Text: stringAt(part, "text")})
		case "image":
			source := mapAt(part, "source")
			if stringAt(source, "type") == "url" {
				blocks = append(blocks, bridgeBlock{Kind: "image", URL: stringAt(source, "url")})
			} else {
				blocks = append(blocks, bridgeBlock{Kind: "image", MediaType: stringAt(source, "media_type"), Data: stringAt(source, "data")})
			}
		case "tool_use":
			blocks = append(blocks, bridgeBlock{
				Kind:      "tool_call",
				ID:        stringAt(part, "id"),
				Name:      stringAt(part, "name"),
				Arguments: part["input"],
			})
		case "tool_result":
			blocks = append(blocks, bridgeBlock{
				Kind:    "tool_result",
				CallID:  stringAt(part, "tool_use_id"),
				Result:  part["content"],
				IsError: boolAt(part, "is_error"),
			})
		}
	}
	return blocks
}

func decodeChatReasoning(message map[string]any) []bridgeBlock {
	value, exists := message["reasoning_content"]
	if !exists {
		value, exists = message["reasoning"]
	}
	text, ok := value.(string)
	if !exists || !ok {
		return nil
	}
	return []bridgeBlock{{
		Kind:      "reasoning",
		Text:      text,
		Signature: stringAt(message, "reasoning_signature"),
	}}
}

func decodeResponsesReasoning(item map[string]any) []bridgeBlock {
	var text strings.Builder
	for _, raw := range asSlice(item["summary"]) {
		part, _ := raw.(map[string]any)
		text.WriteString(stringAt(part, "text"))
	}
	if content := item["content"]; content != nil {
		for _, raw := range asSlice(content) {
			part, _ := raw.(map[string]any)
			text.WriteString(stringAt(part, "text"))
		}
	}
	encrypted := stringAt(item, "encrypted_content")
	if text.Len() == 0 && encrypted == "" {
		return nil
	}
	return []bridgeBlock{{
		Kind:      "reasoning",
		ID:        stringAt(item, "id"),
		Text:      text.String(),
		Encrypted: encrypted,
	}}
}

func encodeResponsesReasoning(block bridgeBlock, completed bool) map[string]any {
	id := block.ID
	if id == "" {
		id = randomID("rs", 12)
	}
	summary := []any{}
	if block.Text != "" {
		summary = append(summary, map[string]any{"type": "summary_text", "text": block.Text})
	}
	item := map[string]any{"id": id, "type": "reasoning", "summary": summary}
	if block.Encrypted != "" {
		item["encrypted_content"] = block.Encrypted
	}
	if completed {
		item["status"] = "completed"
	}
	return item
}

func bridgeReasoningText(blocks []bridgeBlock) (string, bool) {
	if len(blocks) == 0 {
		return "", false
	}
	var text strings.Builder
	for _, block := range blocks {
		text.WriteString(block.Text)
	}
	return text.String(), true
}

func bridgeReasoningSignature(blocks []bridgeBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Signature != "" {
			return blocks[i].Signature
		}
	}
	return ""
}

func encodeChatBlocks(blocks []bridgeBlock) any {
	if len(blocks) == 1 && blocks[0].Kind == "text" {
		return blocks[0].Text
	}
	parts := make([]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Kind {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": block.Text})
		case "image":
			url := block.URL
			if url == "" && block.Data != "" {
				url = "data:" + block.MediaType + ";base64," + block.Data
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		}
	}
	return parts
}

func encodeAnthropicBlocks(blocks []bridgeBlock) []any {
	parts := make([]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Kind {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": block.Text})
		case "image":
			if block.URL != "" {
				parts = append(parts, map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": block.URL}})
			} else {
				parts = append(parts, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": block.MediaType, "data": block.Data}})
			}
		case "tool_call":
			input := block.Arguments
			if input == nil {
				input = parseJSONOrString(block.ArgumentsJSON)
			}
			parts = append(parts, map[string]any{"type": "tool_use", "id": block.ID, "name": block.Name, "input": input})
		case "tool_result":
			parts = append(parts, map[string]any{"type": "tool_result", "tool_use_id": block.CallID, "content": block.Result, "is_error": block.IsError})
		case "reasoning":
			if block.Encrypted != "" {
				parts = append(parts, map[string]any{"type": "redacted_thinking", "data": block.Encrypted})
				continue
			}
			part := map[string]any{"type": "thinking", "thinking": block.Text, "signature": block.Signature}
			parts = append(parts, part)
		}
	}
	return parts
}

func bridgeArgumentsJSON(block bridgeBlock) string {
	if block.ArgumentsJSON != "" {
		return block.ArgumentsJSON
	}
	if block.Arguments == nil {
		return "{}"
	}
	data, err := json.Marshal(block.Arguments)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func bridgeToolResultContent(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if parts, ok := value.([]any); ok {
		var text strings.Builder
		allText := true
		for _, raw := range parts {
			part, ok := raw.(map[string]any)
			if !ok || stringAt(part, "type") != "text" {
				allText = false
				break
			}
			text.WriteString(stringAt(part, "text"))
		}
		if allText {
			return text.String()
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeChatToolChoice(value any) bridgeToolChoice {
	if mode, ok := value.(string); ok {
		return bridgeToolChoice{Mode: mode}
	}
	choice, _ := value.(map[string]any)
	if stringAt(choice, "type") == "function" {
		return bridgeToolChoice{Mode: "named", Name: stringAt(choice, "function", "name")}
	}
	return bridgeToolChoice{}
}

func decodeResponsesToolChoice(value any) bridgeToolChoice {
	if mode, ok := value.(string); ok {
		return bridgeToolChoice{Mode: mode}
	}
	choice, _ := value.(map[string]any)
	if stringAt(choice, "type") == "function" {
		return bridgeToolChoice{Mode: "named", Name: stringAt(choice, "name")}
	}
	return bridgeToolChoice{}
}

func decodeAnthropicToolChoice(value any) bridgeToolChoice {
	choice, _ := value.(map[string]any)
	switch stringAt(choice, "type") {
	case "auto":
		return bridgeToolChoice{Mode: "auto"}
	case "none":
		return bridgeToolChoice{Mode: "none"}
	case "any":
		return bridgeToolChoice{Mode: "required"}
	case "tool":
		return bridgeToolChoice{Mode: "named", Name: stringAt(choice, "name")}
	default:
		return bridgeToolChoice{}
	}
}

func encodeChatToolChoice(choice bridgeToolChoice) any {
	switch choice.Mode {
	case "named":
		return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
	case "auto", "none", "required":
		return choice.Mode
	default:
		return nil
	}
}

func encodeResponsesToolChoice(choice bridgeToolChoice) any {
	switch choice.Mode {
	case "named":
		return map[string]any{"type": "function", "name": choice.Name}
	case "auto", "none", "required":
		return choice.Mode
	default:
		return nil
	}
}

func encodeAnthropicToolChoice(choice bridgeToolChoice) any {
	switch choice.Mode {
	case "named":
		return map[string]any{"type": "tool", "name": choice.Name}
	case "required":
		return map[string]any{"type": "any"}
	case "auto", "none":
		return map[string]any{"type": choice.Mode}
	default:
		return nil
	}
}

func convertResponse(from, to Protocol, body []byte) ([]byte, error) {
	if from == to {
		return append([]byte(nil), body...), nil
	}
	var input map[string]any
	if err := json.Unmarshal(body, &input); err != nil {
		return nil, err
	}
	response, err := decodeBridgeResponse(from, input)
	if err != nil {
		return nil, err
	}
	return json.Marshal(encodeBridgeResponse(to, response))
}

func decodeBridgeResponse(protocol Protocol, input map[string]any) (bridgeResponse, error) {
	response := bridgeResponse{
		ID:      stringAt(input, "id"),
		Model:   stringAt(input, "model"),
		Created: int64At(input, "created"),
	}
	if response.Created == 0 {
		response.Created = int64At(input, "created_at")
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	switch protocol {
	case ProtocolChat:
		choices := sliceAt(input, "choices")
		if len(choices) == 0 {
			return response, fmt.Errorf("chat response contains no choices")
		}
		choice, _ := choices[0].(map[string]any)
		message := mapAt(choice, "message")
		response.Reasoning = decodeChatReasoning(message)
		response.Text = bridgeBlocksText(decodeOpenAIBlocks(message["content"]))
		for _, raw := range sliceAt(message, "tool_calls") {
			call, _ := raw.(map[string]any)
			function := mapAt(call, "function")
			response.Tools = append(response.Tools, bridgeBlock{
				Kind:          "tool_call",
				ID:            stringAt(call, "id"),
				Name:          stringAt(function, "name"),
				ArgumentsJSON: stringAt(function, "arguments"),
			})
		}
		response.Stop = stringAt(choice, "finish_reason")
		response.Usage = decodeOpenAIUsage(mapAt(input, "usage"))
	case ProtocolResponses:
		for _, raw := range sliceAt(input, "output") {
			item, _ := raw.(map[string]any)
			switch stringAt(item, "type") {
			case "reasoning":
				response.Reasoning = append(response.Reasoning, decodeResponsesReasoning(item)...)
			case "message":
				response.Text += bridgeBlocksText(decodeOpenAIBlocks(item["content"]))
			case "function_call":
				response.Tools = append(response.Tools, bridgeBlock{
					Kind:          "tool_call",
					ID:            firstString(stringAt(item, "call_id"), stringAt(item, "id")),
					Name:          stringAt(item, "name"),
					ArgumentsJSON: stringAt(item, "arguments"),
				})
			}
		}
		response.Stop = "stop"
		if len(response.Tools) > 0 {
			response.Stop = "tool_calls"
		}
		if stringAt(input, "status") == "incomplete" {
			response.Stop = "length"
		}
		response.Usage = decodeOpenAIUsage(mapAt(input, "usage"))
	case ProtocolAnthropic:
		for _, block := range decodeAnthropicBlocks(input["content"]) {
			if block.Kind == "reasoning" {
				response.Reasoning = append(response.Reasoning, block)
			} else if block.Kind == "text" {
				response.Text += block.Text
			} else if block.Kind == "tool_call" {
				response.Tools = append(response.Tools, block)
			}
		}
		response.Stop = stringAt(input, "stop_reason")
		response.Usage = decodeAnthropicUsage(mapAt(input, "usage"))
	default:
		return response, fmt.Errorf("unsupported response protocol %q", protocol)
	}
	return response, nil
}

func encodeBridgeResponse(protocol Protocol, response bridgeResponse) map[string]any {
	if response.ID == "" {
		response.ID = randomID("resp", 12)
	}
	switch protocol {
	case ProtocolChat:
		message := map[string]any{"role": "assistant", "content": response.Text}
		if value, ok := bridgeReasoningText(response.Reasoning); ok {
			message["reasoning_content"] = value
		}
		if signature := bridgeReasoningSignature(response.Reasoning); signature != "" {
			message["reasoning_signature"] = signature
		}
		if len(response.Tools) > 0 {
			calls := make([]any, 0, len(response.Tools))
			for _, tool := range response.Tools {
				calls = append(calls, map[string]any{
					"id":   tool.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tool.Name,
						"arguments": bridgeArgumentsJSON(tool),
					},
				})
			}
			message["tool_calls"] = calls
		}
		return map[string]any{
			"id":      asPrefix(response.ID, "chatcmpl"),
			"object":  "chat.completion",
			"created": response.Created,
			"model":   response.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       message,
				"finish_reason": chatStop(response.Stop),
			}},
			"usage": openAIUsage(response.Usage),
		}
	case ProtocolResponses:
		output := make([]any, 0, len(response.Reasoning)+len(response.Tools)+1)
		for _, reasoning := range response.Reasoning {
			output = append(output, encodeResponsesReasoning(reasoning, true))
		}
		if response.Text != "" {
			output = append(output, map[string]any{
				"id":     randomID("msg", 12),
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []any{map[string]any{
					"type":        "output_text",
					"text":        response.Text,
					"annotations": []any{},
				}},
			})
		}
		for _, tool := range response.Tools {
			output = append(output, map[string]any{
				"id":        randomID("fc", 12),
				"type":      "function_call",
				"status":    "completed",
				"call_id":   tool.ID,
				"name":      tool.Name,
				"arguments": bridgeArgumentsJSON(tool),
			})
		}
		return map[string]any{
			"id":                 asPrefix(response.ID, "resp"),
			"object":             "response",
			"created_at":         response.Created,
			"status":             "completed",
			"model":              response.Model,
			"output":             output,
			"error":              nil,
			"incomplete_details": nil,
			"usage": map[string]any{
				"input_tokens":  response.Usage.Input,
				"output_tokens": response.Usage.Output,
				"total_tokens":  response.Usage.Total,
				"input_tokens_details": map[string]any{
					"cached_tokens": response.Usage.Cached,
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": response.Usage.Reasoning,
				},
			},
		}
	case ProtocolAnthropic:
		content := make([]any, 0, len(response.Reasoning)+len(response.Tools)+1)
		content = append(content, encodeAnthropicBlocks(response.Reasoning)...)
		if response.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": response.Text})
		}
		for _, tool := range response.Tools {
			input := tool.Arguments
			if input == nil {
				input = parseJSONOrString(tool.ArgumentsJSON)
			}
			content = append(content, map[string]any{"type": "tool_use", "id": tool.ID, "name": tool.Name, "input": input})
		}
		return map[string]any{
			"id":            asPrefix(response.ID, "msg"),
			"type":          "message",
			"role":          "assistant",
			"model":         response.Model,
			"content":       content,
			"stop_reason":   anthropicStop(response.Stop),
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":            response.Usage.Input,
				"output_tokens":           response.Usage.Output,
				"cache_read_input_tokens": response.Usage.Cached,
			},
		}
	default:
		return map[string]any{}
	}
}

func decodeOpenAIUsage(usage map[string]any) bridgeUsage {
	input := firstNonZero(intAt(usage, "prompt_tokens"), intAt(usage, "input_tokens"))
	output := firstNonZero(intAt(usage, "completion_tokens"), intAt(usage, "output_tokens"))
	total := intAt(usage, "total_tokens")
	if total == 0 {
		total = input + output
	}
	cached := firstNonZero(intAt(usage, "prompt_tokens_details", "cached_tokens"), intAt(usage, "input_tokens_details", "cached_tokens"))
	reasoning := firstNonZero(intAt(usage, "completion_tokens_details", "reasoning_tokens"), intAt(usage, "output_tokens_details", "reasoning_tokens"))
	return bridgeUsage{Input: input, Output: output, Total: total, Cached: cached, Reasoning: reasoning}
}

func decodeAnthropicUsage(usage map[string]any) bridgeUsage {
	input := intAt(usage, "input_tokens")
	output := intAt(usage, "output_tokens")
	return bridgeUsage{
		Input:  input,
		Output: output,
		Total:  input + output,
		Cached: intAt(usage, "cache_read_input_tokens"),
	}
}

func openAIUsage(usage bridgeUsage) map[string]any {
	return map[string]any{
		"prompt_tokens":     usage.Input,
		"completion_tokens": usage.Output,
		"total_tokens":      usage.Total,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": usage.Cached,
		},
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": usage.Reasoning,
		},
	}
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func normalizeResponsesRole(role string) string {
	if role == "assistant" {
		return "assistant"
	}
	return "user"
}

func chatStop(stop string) string {
	switch stop {
	case "tool_use", "tool_calls":
		return "tool_calls"
	case "max_tokens", "length":
		return "length"
	default:
		return "stop"
	}
}

func anthropicStop(stop string) string {
	switch stop {
	case "tool_use", "tool_calls":
		return "tool_use"
	case "max_tokens", "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func schemaOrDefault(value any) any {
	if value == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return value
}

func reasoningEffort(value any) any {
	if object, ok := value.(map[string]any); ok {
		return firstAny(object["effort"], object["type"])
	}
	return value
}

func bridgeBlocksText(blocks []bridgeBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Kind == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func parseJSONOrString(value string) any {
	if value == "" {
		return map[string]any{}
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func asPrefix(id, prefix string) string {
	if strings.HasPrefix(id, prefix+"_") {
		return id
	}
	for _, current := range []string{"chatcmpl_", "resp_", "msg_"} {
		id = strings.TrimPrefix(id, current)
	}
	return prefix + "_" + id
}

func put(object map[string]any, key string, value any) {
	if value != nil {
		object[key] = value
	}
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	data, _ := json.Marshal(input)
	var output map[string]any
	_ = json.Unmarshal(data, &output)
	return output
}

func asSlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	if value == nil {
		return nil
	}
	return []any{value}
}

func sliceAt(object map[string]any, path ...string) []any {
	values, _ := anyAt(object, path...).([]any)
	return values
}

func mapAt(object map[string]any, path ...string) map[string]any {
	value, _ := anyAt(object, path...).(map[string]any)
	return value
}

func stringAt(object map[string]any, path ...string) string {
	value, _ := anyAt(object, path...).(string)
	return value
}

func boolAt(object map[string]any, path ...string) bool {
	value, _ := anyAt(object, path...).(bool)
	return value
}

func intAt(object map[string]any, path ...string) int {
	value := anyAt(object, path...)
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	case json.Number:
		integer, _ := number.Int64()
		return int(integer)
	default:
		return 0
	}
}

func int64At(object map[string]any, path ...string) int64 {
	return int64(intAt(object, path...))
}

func anyAt(object map[string]any, path ...string) any {
	var current any = object
	for _, key := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[key]
	}
	return current
}
