package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

const testTimeout = 3 * time.Second

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type testPeer struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func newTestClient(t *testing.T, opts ...Option) (*Client, *testPeer) {
	t.Helper()
	c1, c2 := net.Pipe()
	client := NewClient(c1, c1, opts...)
	client.Start()
	peer := &testPeer{t: t, conn: c2, r: bufio.NewReader(c2)}
	t.Cleanup(func() {
		_ = client.Close()
		_ = c1.Close()
		_ = c2.Close()
	})
	return client, peer
}

func (p *testPeer) read() envelope {
	p.t.Helper()
	type result struct {
		line []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := p.r.ReadBytes('\n')
		ch <- result{line: line, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			p.t.Fatalf("read message: %v", res.err)
		}
		var msg envelope
		if err := json.Unmarshal(res.line, &msg); err != nil {
			p.t.Fatalf("decode message %q: %v", res.line, err)
		}
		return msg
	case <-time.After(testTimeout):
		p.t.Fatal("timeout waiting for a message from the client")
		return envelope{}
	}
}

func (p *testPeer) writeLine(line string) {
	p.t.Helper()
	ch := make(chan error, 1)
	go func() {
		_, err := io.WriteString(p.conn, line+"\n")
		ch <- err
	}()
	select {
	case err := <-ch:
		if err != nil {
			p.t.Fatalf("write message: %v", err)
		}
	case <-time.After(testTimeout):
		p.t.Fatal("timeout writing a message to the client")
	}
}

func (p *testPeer) writeJSON(v any) {
	p.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		p.t.Fatalf("marshal message: %v", err)
	}
	p.writeLine(string(data))
}

func (p *testPeer) writeChunked(payload string, chunk int) {
	p.t.Helper()
	for i := 0; i < len(payload); i += chunk {
		end := min(i+chunk, len(payload))
		if _, err := io.WriteString(p.conn, payload[i:end]); err != nil {
			p.t.Fatalf("write chunk: %v", err)
		}
	}
}

type callResult struct {
	raw json.RawMessage
	err error
}

func startCall(client *Client, method string, params any) <-chan callResult {
	ch := make(chan callResult, 1)
	go func() {
		raw, err := client.Call(context.Background(), method, params)
		ch <- callResult{raw: raw, err: err}
	}()
	return ch
}

func waitCall(t *testing.T, ch <-chan callResult) callResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for call result")
		return callResult{}
	}
}

func TestCall(t *testing.T) {
	client, peer := newTestClient(t)
	resCh := startCall(client, "initialize", InitializeParams{
		ProtocolVersion:    ProtocolVersion,
		ClientCapabilities: DefaultClientCapabilities(),
	})

	req := peer.read()
	if req.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q", req.JSONRPC)
	}
	if req.Method != "initialize" {
		t.Errorf("method = %q", req.Method)
	}
	if string(req.ID) != "1" {
		t.Errorf("id = %s, want 1", req.ID)
	}
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d", params.ProtocolVersion)
	}
	if params.ClientCapabilities.Terminal || params.ClientCapabilities.Fs.ReadTextFile {
		t.Errorf("client capabilities must not grant fs/terminal: %+v", params.ClientCapabilities)
	}

	peer.writeJSON(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result"`
	}{"2.0", req.ID, map[string]any{"protocolVersion": ProtocolVersion}})

	res := waitCall(t, resCh)
	if res.err != nil {
		t.Fatalf("call: %v", res.err)
	}
	var init InitializeResult
	if err := json.Unmarshal(res.raw, &init); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if init.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d", init.ProtocolVersion)
	}
}

func TestCallErrorPreservesCodeAndData(t *testing.T) {
	client, peer := newTestClient(t)
	resCh := startCall(client, "session/new", map[string]any{"cwd": "/tmp"})
	req := peer.read()

	peer.writeJSON(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   any             `json:"error"`
	}{"2.0", req.ID, map[string]any{
		"code":    -32000,
		"message": "authentication required",
		"data":    map[string]any{"authMethods": []string{"pi_terminal_login"}},
	}})

	res := waitCall(t, resCh)
	if res.err == nil {
		t.Fatal("expected an error")
	}
	var rpcErr *RPCError
	if !errors.As(res.err, &rpcErr) {
		t.Fatalf("error type %T, want *RPCError", res.err)
	}
	if rpcErr.Code != -32000 {
		t.Errorf("code = %d", rpcErr.Code)
	}
	if !bytes.Contains(rpcErr.Data, []byte("pi_terminal_login")) {
		t.Errorf("data = %s", rpcErr.Data)
	}
}

func TestConcurrentCallsOutOfOrder(t *testing.T) {
	client, peer := newTestClient(t)
	first := startCall(client, "first", nil)
	second := startCall(client, "second", nil)

	ids := map[string]json.RawMessage{}
	for range 2 {
		req := peer.read()
		ids[req.Method] = req.ID
	}

	respond := func(method string) {
		peer.writeJSON(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  any             `json:"result"`
		}{"2.0", ids[method], map[string]any{"method": method}})
	}
	respond("second")
	respond("first")

	for method, ch := range map[string]<-chan callResult{"first": first, "second": second} {
		res := waitCall(t, ch)
		if res.err != nil {
			t.Fatalf("%s: %v", method, res.err)
		}
		var got struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(res.raw, &got); err != nil {
			t.Fatalf("%s result: %v", method, err)
		}
		if got.Method != method {
			t.Errorf("%s got result for %s", method, got.Method)
		}
	}
}

func TestNotification(t *testing.T) {
	type notification struct {
		method string
		params json.RawMessage
	}
	got := make(chan notification, 1)
	_, peer := newTestClient(t, WithNotificationHandler(func(method string, params json.RawMessage) {
		got <- notification{method: method, params: params}
	}))

	peer.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params":  map[string]any{"sessionUpdate": "agent_message_chunk"},
	})

	select {
	case n := <-got:
		if n.method != "session/update" {
			t.Errorf("method = %q", n.method)
		}
		var params struct {
			SessionUpdate string `json:"sessionUpdate"`
		}
		if err := json.Unmarshal(n.params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if params.SessionUpdate != "agent_message_chunk" {
			t.Errorf("sessionUpdate = %q", params.SessionUpdate)
		}
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for notification")
	}
}

func TestPermissionRequest(t *testing.T) {
	paramsCh := make(chan RequestPermissionParams, 1)
	_, peer := newTestClient(t, WithRequestHandler(func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "session/request_permission" {
			return nil, &RPCError{Code: CodeMethodNotFound, Message: "unexpected method"}
		}
		var p RequestPermissionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		paramsCh <- p
		return SelectedPermission("allow-once"), nil
	}))

	peer.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      42,
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "s1",
			"toolCall":  map[string]any{"toolCallId": "t1", "title": "Read file"},
			"options": []map[string]any{
				{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
				{"optionId": "reject", "name": "Reject", "kind": "reject_once"},
			},
		},
	})

	var params RequestPermissionParams
	select {
	case params = <-paramsCh:
	case <-time.After(testTimeout):
		t.Fatal("handler was not called")
	}
	if params.SessionID != "s1" || len(params.Options) != 2 {
		t.Errorf("params = %+v", params)
	}

	resp := peer.read()
	if string(resp.ID) != "42" {
		t.Errorf("id = %s", resp.ID)
	}
	var result RequestPermissionResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Outcome.Outcome != "selected" || result.Outcome.OptionID != "allow-once" {
		t.Errorf("outcome = %+v", result.Outcome)
	}
}

func TestUnknownRequestGetsMethodNotFound(t *testing.T) {
	_, peer := newTestClient(t)
	peer.writeJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      "abc",
		"method":  "fs/read_text_file",
		"params":  map[string]any{"path": "/etc/passwd"},
	})

	resp := peer.read()
	if string(resp.ID) != `"abc"` {
		t.Errorf("id = %s, want \"abc\"", resp.ID)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("error = %+v, want -32601", resp.Error)
	}
}

func TestProtocolErrorDoesNotStopReadLoop(t *testing.T) {
	errCh := make(chan error, 1)
	noteCh := make(chan string, 1)
	_, peer := newTestClient(t,
		WithProtocolErrorHandler(func(_ []byte, err error) { errCh <- err }),
		WithNotificationHandler(func(method string, _ json.RawMessage) { noteCh <- method }),
	)

	peer.writeLine("this is not json")
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("protocol error is nil")
		}
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for protocol error")
	}

	peer.writeJSON(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{}})
	select {
	case method := <-noteCh:
		if method != "session/update" {
			t.Errorf("method = %q", method)
		}
	case <-time.After(testTimeout):
		t.Fatal("read loop stopped after a protocol error")
	}
}

func TestCallHandlesCRLFU2028AndChunks(t *testing.T) {
	client, peer := newTestClient(t)
	resCh := startCall(client, "session/prompt", map[string]any{"prompt": "hi"})
	req := peer.read()

	// Blank lines are ignored between messages.
	peer.writeLine("")

	const content = `{"jsonrpc":"2.0","id":` + `%s` + `,"result":{"text":"a` + "\u2028" + `b"}}`
	payload := fmt.Sprintf(content, req.ID) + "\r\n"
	peer.writeChunked(payload, 1)

	res := waitCall(t, resCh)
	if res.err != nil {
		t.Fatalf("call: %v", res.err)
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(res.raw, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.Text != "a\u2028b" {
		t.Errorf("text = %q", got.Text)
	}
}

func TestCloseRejectsPendingAndLaterCalls(t *testing.T) {
	client, peer := newTestClient(t)
	resCh := startCall(client, "session/prompt", nil)
	peer.read()

	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	res := waitCall(t, resCh)
	if !errors.Is(res.err, ErrClosed) {
		t.Fatalf("pending call error = %v, want ErrClosed", res.err)
	}

	_, err := client.Call(context.Background(), "session/new", nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("call after Close error = %v, want ErrClosed", err)
	}
}

func TestCloseUnblocksDone(t *testing.T) {
	client, _ := newTestClient(t)
	client.Close()
	select {
	case <-client.Done():
	case <-time.After(testTimeout):
		t.Fatal("Done was not closed")
	}
}
