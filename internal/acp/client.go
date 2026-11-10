// Package acp implements the client half of the Agent Client Protocol:
// newline-delimited JSON-RPC 2.0 over a pair of streams (usually an agent's
// stdin/stdout), request routing and permission responses.
//
// It knows nothing about processes or UI. internal/session owns the process
// lifecycle; internal/ui owns rendering.
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// ErrClosed is returned by calls racing with Close or with an agent that
// closed its output.
var ErrClosed = errors.New("acp: client closed")

// JSON-RPC error codes used by the client.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// RPCError is a JSON-RPC error returned by the agent or by a request handler.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("acp: rpc error %d: %s", e.Code, e.Message)
}

// RequestHandler answers agent -> client requests such as
// session/request_permission. Returning an *RPCError preserves its code and
// data. Without a handler every request is answered with -32601.
//
// The handler runs on its own goroutine and may block (for example while the
// user answers a permission dialog).
type RequestHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// NotificationHandler receives agent -> client notifications such as
// session/update. It runs on the read goroutine, so it must not block; hand
// slow work to another goroutine or a queue.
type NotificationHandler func(method string, params json.RawMessage)

// ProtocolErrorHandler reports a malformed line or envelope. The client keeps
// reading after reporting one.
type ProtocolErrorHandler func(line []byte, err error)

// Option configures a Client.
type Option func(*Client)

// WithRequestHandler handles agent -> client requests.
func WithRequestHandler(h RequestHandler) Option {
	return func(c *Client) { c.requestHandler = h }
}

// WithNotificationHandler handles agent -> client notifications.
func WithNotificationHandler(h NotificationHandler) Option {
	return func(c *Client) { c.notificationHandler = h }
}

// WithProtocolErrorHandler reports malformed input without stopping the read
// loop.
func WithProtocolErrorHandler(h ProtocolErrorHandler) Option {
	return func(c *Client) { c.protocolErrorHandler = h }
}

type response struct {
	result json.RawMessage
	err    error
}

// Client speaks ACP over one reader and one writer.
type Client struct {
	r io.Reader
	w io.Writer

	requestHandler       RequestHandler
	notificationHandler  NotificationHandler
	protocolErrorHandler ProtocolErrorHandler

	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	closeOnce sync.Once
	done      chan struct{}

	nextID atomic.Int64

	writeMu sync.Mutex

	mu       sync.Mutex
	pending  map[string]chan response
	closed   bool
	closeErr error
}

// NewClient creates a client. Call Start to begin reading. The caller owns r
// and w (usually an agent's stdout and stdin) and should close them with the
// process.
func NewClient(r io.Reader, w io.Writer, opts ...Option) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		r:       r,
		w:       w,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		pending: make(map[string]chan response),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Start begins the read loop. It is safe to call more than once.
func (c *Client) Start() {
	c.startOnce.Do(func() {
		go c.readLoop()
	})
}

// Close stops the read loop once the underlying streams unblock, rejects
// pending calls with ErrClosed, and answers later calls with ErrClosed.
func (c *Client) Close() error {
	c.cancel()
	c.shutdown(ErrClosed)
	return nil
}

// Done is closed when the client shuts down.
func (c *Client) Done() <-chan struct{} { return c.done }

// Call sends a request and waits for its response. Cancelling ctx abandons
// the wait; it does not send session/cancel (that is the session's job).
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	paramsRaw, err := marshalParams(params)
	if err != nil {
		return nil, fmt.Errorf("acp: marshal %s params: %w", method, err)
	}

	id := c.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	ch := make(chan response, 1)

	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		return nil, err
	}
	c.pending[key] = ch
	c.mu.Unlock()

	req := wireRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(key),
		Method:  method,
		Params:  paramsRaw,
	}
	if err := c.writeMessage(req); err != nil {
		c.removePending(key)
		return nil, err
	}

	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		c.removePending(key)
		return nil, ctx.Err()
	case <-c.done:
		c.removePending(key)
		return nil, c.err()
	}
}

// Notify sends a notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	paramsRaw, err := marshalParams(params)
	if err != nil {
		return fmt.Errorf("acp: marshal %s params: %w", method, err)
	}
	return c.writeMessage(wireNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	})
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(params)
}

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type wireNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type wireMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

func (c *Client) writeMessage(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("acp: marshal message: %w", err)
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	n, err := c.w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func (c *Client) readLoop() {
	lr := newLineReader(c.r)
	for {
		line, err := lr.next()
		if err != nil {
			c.shutdown(fmt.Errorf("acp: read: %w", err))
			return
		}
		if len(line) == 0 {
			continue
		}
		c.handleLine(line)
	}
}

func (c *Client) handleLine(line []byte) {
	var msg wireMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		c.reportProtocolError(line, fmt.Errorf("invalid JSON: %w", err))
		return
	}

	if msg.Method != "" {
		if msg.ID != nil {
			c.handleRequest(&msg)
			return
		}
		c.handleNotification(msg.Method, msg.Params)
		return
	}

	if msg.ID == nil {
		c.reportProtocolError(line, errors.New("message has neither method nor id"))
		return
	}

	key := string(*msg.ID)
	ch := c.takePending(key)
	if ch == nil {
		c.reportProtocolError(line, fmt.Errorf("response for unknown request id %s", key))
		return
	}

	switch {
	case msg.Error != nil:
		ch <- response{err: msg.Error}
	case len(msg.Result) > 0:
		ch <- response{result: msg.Result}
	default:
		ch <- response{err: fmt.Errorf("acp: response %s has neither result nor error", key)}
	}
}

func (c *Client) handleRequest(msg *wireMessage) {
	go func() {
		result, err := c.callRequestHandler(msg.Method, msg.Params)
		if err != nil {
			c.respondError(*msg.ID, err)
			return
		}
		c.respondResult(*msg.ID, result)
	}()
}

func (c *Client) callRequestHandler(method string, params json.RawMessage) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("acp: request handler panic: %v", r)
		}
	}()
	if c.requestHandler == nil {
		return nil, &RPCError{Code: CodeMethodNotFound, Message: "method not supported: " + method}
	}
	return c.requestHandler(c.ctx, method, params)
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	if c.notificationHandler == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			c.reportProtocolError(nil, fmt.Errorf("acp: notification handler panic: %v", r))
		}
	}()
	c.notificationHandler(method, params)
}

func (c *Client) respondResult(id json.RawMessage, result any) {
	if result == nil {
		result = struct{}{}
	}
	if err := c.writeMessage(wireResponse{JSONRPC: "2.0", ID: id, Result: result}); err != nil {
		c.reportProtocolError(nil, fmt.Errorf("acp: write response: %w", err))
	}
}

func (c *Client) respondError(id json.RawMessage, err error) {
	if err := c.writeMessage(wireResponse{JSONRPC: "2.0", ID: id, Error: asRPCError(err)}); err != nil {
		c.reportProtocolError(nil, fmt.Errorf("acp: write error response: %w", err))
	}
}

func asRPCError(err error) *RPCError {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr != nil {
		return rpcErr
	}
	return &RPCError{Code: CodeInternalError, Message: err.Error()}
}

func (c *Client) takePending(key string) chan response {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.pending[key]
	delete(c.pending, key)
	return ch
}

func (c *Client) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *Client) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr != nil {
		return c.closeErr
	}
	return ErrClosed
}

func (c *Client) shutdown(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrClosed
		}

		c.mu.Lock()
		c.closed = true
		c.closeErr = err
		pending := c.pending
		c.pending = make(map[string]chan response)
		c.mu.Unlock()

		c.cancel()
		close(c.done)
		for _, ch := range pending {
			ch <- response{err: err}
		}
	})
}

func (c *Client) reportProtocolError(line []byte, err error) {
	if c.protocolErrorHandler == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	var copyLine []byte
	if line != nil {
		copyLine = append([]byte(nil), line...)
	}
	c.protocolErrorHandler(copyLine, err)
}
