package proxy

import (
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// AgentEvent represents a parsed event from a Claude API stream.
type AgentEvent struct {
	StreamID       string
	TerminalID     string
	IsSubagent     bool
	ParentStreamID string // set by main.go when parent is inferred
	Type           string // "stream_start", "thinking", "text", "tool_start", "tool_delta", "tool_end", "stream_end"
	Content        string
	ToolName       string
}

// StreamRegistry tracks active API streams per terminal.
type StreamRegistry struct {
	mu            sync.Mutex
	activeStreams  map[string]int // terminalID -> count of active streams
	Events        chan AgentEvent
}

func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{
		activeStreams: make(map[string]int),
		Events:       make(chan AgentEvent, 256),
	}
}

// StartStream registers a new API stream for the given terminal.
// Returns a streamID and whether this is a subagent stream.
func (sr *StreamRegistry) StartStream(terminalID string) (streamID string, isSubagent bool) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	streamID = uuid.New().String()
	count := sr.activeStreams[terminalID]
	isSubagent = count > 0
	sr.activeStreams[terminalID] = count + 1
	return
}

// EndStream unregisters a stream.
func (sr *StreamRegistry) EndStream(terminalID string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if count := sr.activeStreams[terminalID]; count > 1 {
		sr.activeStreams[terminalID] = count - 1
	} else {
		delete(sr.activeStreams, terminalID)
	}
}

// TerminalProxy holds per-terminal proxy state.
type TerminalProxy struct {
	TerminalID string
	Addr       string // "host:port" the proxy listens on
	Port       int
	registry   *StreamRegistry
	stop       atomic.Bool
}
