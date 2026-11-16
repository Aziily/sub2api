package antigravity

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRewriteCodexPromptForAntigravity(t *testing.T) {
	body := []byte(`{"model":"gemini-4-argon-high","request":{"systemInstruction":{"parts":[{"text":"You are Codex, an agent based on GPT-5. Keep helping."},{"text":"YOU ARE A CODING AGENT RUNNING IN THE CODEX CLI, a terminal assistant."},{"text":"You are Codex, a coding\u200b agent based on GPT-5."}]},"contents":[{"role":"user","parts":[{"text":"<system-reminder>You are Codex, an agent based on GPT-5.</system-reminder>"}]},{"role":"model","parts":[{"text":"You are Codex, an agent based on GPT-5."}]},{"role":"user","parts":[{"functionResponse":{"name":"tool","response":{"text":"You are Codex, an agent based on GPT-5."}}}]}]}}`)
	got := RewriteCodexPromptForAntigravity(body)
	require.Equal(t, "You are Codex, an agent built on GPT-5. Keep helping.", gjson.GetBytes(got, "request.systemInstruction.parts.0.text").String())
	require.Equal(t, "YOU're A CODING AGENT RUNNING IN THE CODEX CLI, a terminal assistant.", gjson.GetBytes(got, "request.systemInstruction.parts.1.text").String())
	require.Equal(t, "You are Codex, a coding agent built on GPT-5.", gjson.GetBytes(got, "request.systemInstruction.parts.2.text").String())
	require.Equal(t, "<system-reminder>You are Codex, an agent built on GPT-5.</system-reminder>", gjson.GetBytes(got, "request.contents.0.parts.0.text").String())
	require.Equal(t, "You are Codex, an agent based on GPT-5.", gjson.GetBytes(got, "request.contents.1.parts.0.text").String())
	require.Equal(t, "You are Codex, an agent based on GPT-5.", gjson.GetBytes(got, "request.contents.2.parts.0.functionResponse.response.text").String())
	require.Equal(t, string(got), string(RewriteCodexPromptForAntigravity(got)))
}

func TestRewriteCodexPromptLeavesUnrelatedPayload(t *testing.T) {
	body := []byte(`{"request":{"systemInstruction":{"parts":[{"text":"You are Codex, based on GPT-6."}]}}}`)
	require.Equal(t, string(body), string(RewriteCodexPromptForAntigravity(body)))
	invalid := []byte(`{"request":`)
	require.Equal(t, string(invalid), string(RewriteCodexPromptForAntigravity(invalid)))
}

func TestNewAPIRequestRewritesOnlyGenerationActions(t *testing.T) {
	body := []byte(`{"request":{"systemInstruction":{"parts":[{"text":"You are Codex, an agent based on GPT-5."}]}}}`)
	for _, action := range []string{"generateContent", "streamGenerateContent"} {
		req, err := NewAPIRequestWithURL(context.Background(), "https://example.test", action, "token", body)
		require.NoError(t, err)
		forwarded, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Contains(t, string(forwarded), "built on GPT-5")
	}
	req, err := NewAPIRequestWithURL(context.Background(), "https://example.test", "countTokens", "token", body)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, string(body), string(forwarded))
}
