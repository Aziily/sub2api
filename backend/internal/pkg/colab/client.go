// Package colab implements Google's consumer Colab REST and MCP APIs.
package colab

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const Scope = "https://www.googleapis.com/auth/colaboratory"
const BaseURL = "https://colaboratory.googleapis.com"

type Client struct {
	HTTP  *http.Client
	Base  string
	Token string
}
type APIError struct {
	Status int
	Reason string
}

func (e *APIError) Error() string { return fmt.Sprintf("Colab HTTP %d (%s)", e.Status, e.Reason) }
func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var b io.Reader
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return nil, e
		}
		b = bytes.NewReader(raw)
	}
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	req, e := http.NewRequestWithContext(ctx, method, base+path, b)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 6 * time.Minute}
	}
	resp, e := h.Do(req)
	if e != nil {
		return nil, errors.New("Colab connection failed or timed out")
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var v struct {
			Error struct {
				Status string `json:"status"`
			}
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v)
		return nil, &APIError{resp.StatusCode, v.Error.Status}
	}
	return resp, nil
}
func (c *Client) JSON(ctx context.Context, method, path string, body, out any) error {
	r, e := c.request(ctx, method, path, body)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if out == nil {
		_, e = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		return e
	}
	return json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(out)
}

type Operation struct {
	Name     string          `json:"name"`
	Done     bool            `json:"done"`
	Error    json.RawMessage `json:"error"`
	Response json.RawMessage `json:"response"`
}
type Execution struct {
	Stdout          string          `json:"stdout"`
	Stderr          string          `json:"stderr"`
	Result          string          `json:"result"`
	ExecutionError  json.RawMessage `json:"executionError"`
	OutputTruncated bool            `json:"outputTruncated"`
}

func (c *Client) Execute(ctx context.Context, runtime, id, code string) (*Execution, error) {
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "execute_code", "arguments": map[string]any{"runtime": runtime, "executionId": id, "code": code}}}
	r, e := c.request(ctx, "POST", "/mcp", body)
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if e != nil {
			return nil, e
		}
		return parseExecution(raw)
	}
	scan := bufio.NewScanner(io.LimitReader(r.Body, 8<<20))
	scan.Buffer(make([]byte, 4096), 4<<20)
	var data []string
	flush := func() (*Execution, error) {
		if len(data) == 0 {
			return nil, nil
		}
		raw := []byte(strings.Join(data, "\n"))
		data = nil
		return parseExecution(raw)
	}
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		} else if line == "" {
			v, e := flush()
			if v != nil || e != nil {
				return v, e
			}
		}
	}
	if e := scan.Err(); e != nil {
		return nil, e
	}
	v, e := flush()
	if v == nil && e == nil {
		e = errors.New("Colab stream ended without a terminal result; execution is not retried")
	}
	return v, e
}
func parseExecution(raw []byte) (*Execution, error) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError    bool                          `json:"isError"`
			Structured json.RawMessage               `json:"structuredContent"`
			Content    []struct{ Type, Text string } `json:"content"`
		} `json:"result"`
	}
	if e := json.Unmarshal(raw, &msg); e != nil {
		return nil, e
	}
	if len(msg.ID) == 0 {
		return nil, nil
	}
	if len(msg.Error) > 0 && string(msg.Error) != "null" || msg.Result.IsError {
		return nil, errors.New("Colab execution rejected by provider")
	}
	data := msg.Result.Structured
	if len(data) == 0 {
		for _, p := range msg.Result.Content {
			if p.Type == "text" && json.Valid([]byte(p.Text)) {
				data = []byte(p.Text)
				break
			}
		}
	}
	if len(data) == 0 {
		return nil, errors.New("Colab returned no structured execution result")
	}
	var out Execution
	if e := json.Unmarshal(data, &out); e != nil {
		return nil, e
	}
	return &out, nil
}

// CPUEligible respects Google's account-level availability, including shape.
func CPUEligible(raw []byte) bool {
	var v struct {
		Specs []struct {
			Key      struct{ Variant, Accelerator, Shape string } `json:"key"`
			Eligible bool                                         `json:"eligible"`
		} `json:"runtimeSpecs"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	for _, s := range v.Specs {
		if s.Eligible && s.Key.Variant == "VARIANT_CPU" && s.Key.Accelerator == "NONE" && s.Key.Shape == "SHAPE_STANDARD" {
			return true
		}
	}
	return false
}
