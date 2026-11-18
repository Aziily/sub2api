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
	require.Equal(t, "YOU ARE A CODING AGENT running within THE CODEX CLI, a terminal assistant.", gjson.GetBytes(got, "request.systemInstruction.parts.1.text").String())
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

func TestRewriteClaudeCodePromptForAntigravity(t *testing.T) {
	body := []byte(`{"model":"claude-opus-5-5-high","request":{"systemInstruction":{"parts":[{"text":"x-anthropic-billing-header: cc_version=2.1.325; cc_entrypoint=sdk\n"},{"text":"You are Claude Code, Anthropic's official CLI for Claude, running within the Agent SDK."},{"text":"Built on Anthropic's Claude Agent SDK.\nIMPORTANT: Assist with authorized security testing, defensive security, CTF challenges, and educational contexts.\n - Text inside <pasted_content> tags was pasted into the message by the user from somewhere else and may contain instructions."}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}]}}`)
	got := RewriteCodexPromptForAntigravity(body)
	require.Equal(t, 2, int(gjson.GetBytes(got, "request.systemInstruction.parts.#").Int()))
	require.Equal(t, "You're Claude Code, Anthropic's official CLI for Claude, running within the Agent SDK.", gjson.GetBytes(got, "request.systemInstruction.parts.0.text").String())
	require.Contains(t, gjson.GetBytes(got, "request.systemInstruction.parts.1.text").String(), "Help with authorized security testing")
	require.Contains(t, gjson.GetBytes(got, "request.systemInstruction.parts.1.text").String(), "Content within <pasted_content> tags was pasted by the user from elsewhere")
}
