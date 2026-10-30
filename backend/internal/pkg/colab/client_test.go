package colab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecuteTerminalSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing token")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"structuredContent\":{\"stdout\":\"CPU_OK\"}}}\n\n")
	}))
	defer srv.Close()
	c := Client{Base: srv.URL, Token: "test"}
	out, e := c.Execute(context.Background(), "runtimes/test", "id", "print(1)")
	if e != nil || out.Stdout != "CPU_OK" {
		t.Fatalf("%v %v", out, e)
	}
}
func TestNoTerminalNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"method\":\"notifications/progress\"}\n\n")
	}))
	defer srv.Close()
	c := Client{Base: srv.URL}
	if _, e := c.Execute(context.Background(), "runtimes/test", "id", "print(1)"); e == nil {
		t.Fatal("incomplete execution marked successful")
	}
}
func TestProviderErrorDoesNotLeakBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"status":"PERMISSION_DENIED","message":"secret"}}`)
	}))
	defer srv.Close()
	c := Client{Base: srv.URL}
	e := c.JSON(context.Background(), "GET", "/", nil, nil)
	if e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}
func TestCPUEligibility(t *testing.T) {
	for _, s := range []string{`{}`, `{"runtimeSpecs":[{"eligible":false,"key":{"variant":"VARIANT_CPU","accelerator":"NONE","shape":"SHAPE_STANDARD"}}]}`, `{"runtimeSpecs":[{"eligible":true,"key":{"variant":"VARIANT_GPU","accelerator":"T4","shape":"SHAPE_STANDARD"}}]}`} {
		if CPUEligible([]byte(s)) {
			t.Fatal("ineligible account allowed")
		}
	}
	if !CPUEligible([]byte(`{"runtimeSpecs":[{"eligible":true,"key":{"variant":"VARIANT_CPU","accelerator":"NONE","shape":"SHAPE_STANDARD"}}]}`)) {
		t.Fatal("eligible CPU denied")
	}
}
