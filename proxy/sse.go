package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// SSEEvent is a single server-sent event.
type SSEEvent struct {
	Event string
	Data  string
}

// ContentBlock is the content_block field inside a content_block_start event.
type ContentBlock struct {
	Type string `json:"type"` // "thinking", "text", "tool_use"
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ContentBlockStart is the parsed content_block_start event.
type ContentBlockStart struct {
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// Delta is the delta field inside a content_block_delta event.
type Delta struct {
	Type        string `json:"type"` // "thinking_delta", "text_delta", "input_json_delta"
	Thinking    string `json:"thinking,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// ContentBlockDelta is the parsed content_block_delta event.
type ContentBlockDelta struct {
	Index int   `json:"index"`
	Delta Delta `json:"delta"`
}

// ContentBlockStop is the parsed content_block_stop event.
type ContentBlockStop struct {
	Index int `json:"index"`
}

// MessageStart is the parsed message_start event.
type MessageStart struct {
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Role  string `json:"role"`
	} `json:"message"`
}

// ParseSSEStream reads an SSE stream and sends parsed events to the callback.
// It blocks until the reader is exhausted or an error occurs.
func ParseSSEStream(r io.Reader, cb func(SSEEvent)) {
	scanner := bufio.NewScanner(r)
	// SSE can have long data lines (e.g., tool output).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var event string
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = end of event.
			if len(dataLines) > 0 {
				cb(SSEEvent{
					Event: event,
					Data:  strings.Join(dataLines, "\n"),
				})
			}
			event = ""
			dataLines = dataLines[:0]
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		} else if line == "data:" {
			dataLines = append(dataLines, "")
		}
	}

	// Flush any remaining event.
	if len(dataLines) > 0 {
		cb(SSEEvent{
			Event: event,
			Data:  strings.Join(dataLines, "\n"),
		})
	}
}

// ParseAgentEvents converts raw SSE events from the Anthropic API into AgentEvents.
// streamID and terminalID are set on all emitted events.
func ParseAgentEvents(r io.Reader, streamID, terminalID string, isSubagent bool, events chan<- AgentEvent) {
	// Track content blocks by index to know their type.
	blockTypes := map[int]string{}  // index -> "thinking", "text", "tool_use"
	blockNames := map[int]string{}  // index -> tool name (for tool_use)

	emit := func(typ, content, toolName string) {
		events <- AgentEvent{
			StreamID:   streamID,
			TerminalID: terminalID,
			IsSubagent: isSubagent,
			Type:       typ,
			Content:    content,
			ToolName:   toolName,
		}
	}

	ParseSSEStream(r, func(sse SSEEvent) {
		switch sse.Event {
		case "message_start":
			var ms MessageStart
			if json.Unmarshal([]byte(sse.Data), &ms) == nil {
				emit("stream_start", ms.Message.Model, "")
			}

		case "content_block_start":
			var cbs ContentBlockStart
			if json.Unmarshal([]byte(sse.Data), &cbs) == nil {
				blockTypes[cbs.Index] = cbs.ContentBlock.Type
				blockNames[cbs.Index] = cbs.ContentBlock.Name
				if cbs.ContentBlock.Type == "tool_use" {
					emit("tool_start", "", cbs.ContentBlock.Name)
				}
			}

		case "content_block_delta":
			var cbd ContentBlockDelta
			if json.Unmarshal([]byte(sse.Data), &cbd) == nil {
				switch cbd.Delta.Type {
				case "thinking_delta":
					emit("thinking", cbd.Delta.Thinking, "")
				case "text_delta":
					emit("text", cbd.Delta.Text, "")
				case "input_json_delta":
					emit("tool_delta", cbd.Delta.PartialJSON, blockNames[cbd.Index])
				}
			}

		case "content_block_stop":
			var cbs ContentBlockStop
			if json.Unmarshal([]byte(sse.Data), &cbs) == nil {
				if blockTypes[cbs.Index] == "tool_use" {
					emit("tool_end", "", blockNames[cbs.Index])
				}
				delete(blockTypes, cbs.Index)
				delete(blockNames, cbs.Index)
			}

		case "message_stop":
			emit("stream_end", "", "")
		}
	})
}
