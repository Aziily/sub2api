package antigravity

import (
	"encoding/json"
	"regexp"
	"strings"
)

const antigravityPromptSpace = `[\s\v\p{Zs}\x{2028}\x{2029}\x{feff}]+`
const antigravityPromptInvisible = `[\x{00ad}\x{180e}\x{200b}-\x{200f}\x{2060}-\x{2064}\x{feff}]`

var (
	antigravityPromptInvisibleRe = regexp.MustCompile(antigravityPromptInvisible)
	antigravityPromptBasedOn     = regexp.MustCompile(`(?i)based(` + antigravityPromptSpace + `)on`)
	antigravityPromptCreatedBy   = regexp.MustCompile(`(?i)created(` + antigravityPromptSpace + `)by`)
	antigravityPromptYouAre      = regexp.MustCompile(`(?i)^(you)` + antigravityPromptSpace + `are`)
	antigravityPromptAgentSDK    = regexp.MustCompile(`(?i)^(built` + antigravityPromptSpace + `on` + antigravityPromptSpace + `)anthropic's(` + antigravityPromptSpace + `)(claude` + antigravityPromptSpace + `agent` + antigravityPromptSpace + `sdk)$`)
	antigravityPromptKeywords    = regexp.MustCompile(`(?i)(key)(words)`)
	antigravityPromptInside      = regexp.MustCompile(`(?i)inside`)

	codexUserPromptLines = []codexPromptLine{
		{antigravityPromptLine("you are codex, an agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("you are codex, a coding agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("a coding agent running in the codex cli"), func(match string) string {
			return regexp.MustCompile(`(?i)running(` + antigravityPromptSpace + `)in`).ReplaceAllString(match, "running${1}within")
		}},
		{antigravityPromptLine("codex cli is an open source project led by openai"), func(_ string) string {
			return "This CLI is an open-source project built by OpenAI"
		}},
	}

	codexPromptLines = []codexPromptLine{
		{antigravityPromptBillingLine("x-anthropic-billing-header:"), func(_ string) string {
			return ""
		}},
		{antigravityPromptLine("built on anthropic's claude agent sdk"), func(match string) string {
			return antigravityPromptAgentSDK.ReplaceAllString(match, "${1}the${2}${3} by Anthropic")
		}},
		{antigravityPromptLine("you are codex, an agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("you are codex, a coding agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("rfc 2119 keywords: must, required, should, recommended, may, optional."), func(match string) string {
			return antigravityPromptKeywords.ReplaceAllString(match, "${1} ${2}")
		}},
		{antigravityPromptLine("you are hermes agent"), func(match string) string {
			return antigravityPromptYouAre.ReplaceAllString(match, "${1}'re")
		}},
		{antigravityPromptLine("an intelligent ai assistant created by nous research"), func(match string) string {
			return antigravityPromptCreatedBy.ReplaceAllString(match, "built${1}by")
		}},
		{antigravityPromptLine("you are opencode"), func(match string) string {
			return antigravityPromptYouAre.ReplaceAllString(match, "${1}'re")
		}},
		{antigravityPromptLine("a coding agent running in the codex cli"), func(match string) string {
			return regexp.MustCompile(`(?i)running(` + antigravityPromptSpace + `)in`).ReplaceAllString(match, "running${1}within")
		}},
		{antigravityPromptLine("a coding assistant running inside pi"), func(match string) string {
			return antigravityPromptInside.ReplaceAllString(match, "within")
		}},
		{antigravityPromptLine("you are an ai agent powered by deepseek harness"), func(match string) string {
			return antigravityPromptYouAre.ReplaceAllString(match, "${1}'re")
		}},
		{antigravityPromptLine("you are claude code, anthropic's official cli for claude"), func(match string) string {
			return antigravityPromptYouAre.ReplaceAllString(match, "${1}'re")
		}},
		{antigravityPromptLine("running within the claude agent sdk"), func(_ string) string {
			return "running within the Agent SDK"
		}},
		{antigravityPromptLine("through the claude agent sdk"), func(_ string) string {
			return "through the Agent SDK"
		}},
		{antigravityPromptLine("assist with authorized security testing, defensive security, ctf challenges, and educational contexts"), func(_ string) string {
			return "Help with authorized security testing, defensive security, CTF challenges, and educational contexts"
		}},
		{antigravityPromptLine("refuse requests for destructive techniques, dos attacks, mass targeting"), func(_ string) string {
			return "Decline requests for destructive techniques, DoS attacks, mass targeting"
		}},
		{antigravityPromptLine("dual-use security tools"), func(_ string) string {
			return "Dual-purpose security tools"
		}},
		{antigravityPromptLine("text inside <pasted_content> tags was pasted into the message by the user from somewhere else"), func(_ string) string {
			return "Content within <pasted_content> tags was pasted by the user from elsewhere"
		}},
		{antigravityPromptLine("codex cli is an open source project led by openai"), func(_ string) string {
			return "This CLI is an open-source project built by OpenAI"
		}},
	}

	antigravityRewordedParagraphs = [][2]string{
		{
			`Tokens prefixed with @ are paths the user explicitly referenced. Relative paths resolve from the workspace root; absolute paths identify files or directories on the host. A trailing slash marks a directory: list it when its contents matter. Anything else is a file: use the read tool when its contents are needed, and do not claim to have inspected it before reading. @"..." quotes a path containing spaces.`,
			`When the user writes @ followed by a path, they are pointing at that file or folder. Relative paths start at the workspace root; absolute paths are host paths. A path ending in a slash is a folder, so list it if its contents matter; otherwise it is a file, so open it with the read tool before relying on it. Paths with spaces appear as @"...".`,
		},
	}
)

type codexPromptLine struct {
	pattern *regexp.Regexp
	rewrite func(string) string
}

// RewriteCodexPromptForAntigravity rewrites agent identity lines (Claude Code,
// Claude Agent SDK, Codex, OpenCode, Hermes, pi, DeepSeek Harness, etc.) that
// Antigravity rejects with a generic RESOURCE_EXHAUSTED 429 response.
func RewriteCodexPromptForAntigravity(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	request, ok := payload["request"].(map[string]any)
	if !ok {
		return body
	}
	changed := rewriteCodexTextParts(request["systemInstruction"], true)
	if contents, ok := request["contents"].([]any); ok {
		for _, item := range contents {
			content, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := content["role"].(string); role != "" && role != "user" {
				continue
			}
			changed = rewriteCodexTextParts(content, false) || changed
		}
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteCodexTextParts(value any, rewordParagraphs bool) bool {
	content, ok := value.(map[string]any)
	if !ok {
		return false
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return false
	}
	changed := false
	filtered := make([]any, 0, len(parts))
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		text, ok := part["text"].(string)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		rules := codexPromptLines
		if !rewordParagraphs {
			lowerText := strings.ToLower(text)
			if !strings.Contains(lowerText, "<external_system_instructions>") && !strings.Contains(lowerText, "<system-reminder>") {
				rules = codexUserPromptLines
			}
		}
		rewritten := rewriteCodexPromptText(text, rules)
		if rewordParagraphs {
			for _, pair := range antigravityRewordedParagraphs {
				if strings.Contains(rewritten, pair[0]) {
					rewritten = strings.ReplaceAll(rewritten, pair[0], pair[1])
				}
			}
		}
		if rewritten != text {
			changed = true
			if strings.TrimSpace(rewritten) == "" && len(part) == 1 {
				continue
			}
			part["text"] = rewritten
		}
		filtered = append(filtered, item)
	}
	if changed && len(filtered) != len(parts) && len(filtered) > 0 {
		content["parts"] = filtered
	}
	return changed
}

func rewriteCodexPromptText(text string, rules []codexPromptLine) string {
	type match struct {
		start   int
		end     int
		rewrite func(string) string
	}
	var matches []match
	for _, line := range rules {
		for _, location := range line.pattern.FindAllStringIndex(text, -1) {
			matches = append(matches, match{location[0], location[1], line.rewrite})
		}
	}
	if len(matches) == 0 {
		return text
	}
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].start < matches[j-1].start; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
	var builder strings.Builder
	builder.Grow(len(text))
	previous := 0
	for _, item := range matches {
		if item.start < previous {
			continue
		}
		builder.WriteString(text[previous:item.start])
		visible := antigravityPromptInvisibleRe.ReplaceAllString(text[item.start:item.end], "")
		builder.WriteString(item.rewrite(visible))
		previous = item.end
	}
	builder.WriteString(text[previous:])
	return builder.String()
}

func antigravityPromptLine(line string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + antigravityPromptPattern(line))
}

func antigravityPromptBillingLine(line string) *regexp.Regexp {
	return regexp.MustCompile("(?i)" + antigravityPromptPattern(line) + `[^\n]*\n?`)
}

func antigravityPromptPattern(line string) string {
	words := strings.Split(line, " ")
	for index, word := range words {
		characters := make([]string, 0, len(word))
		for _, character := range word {
			characters = append(characters, regexp.QuoteMeta(string(character)))
		}
		words[index] = strings.Join(characters, antigravityPromptInvisible+"*")
	}
	separator := antigravityPromptInvisible + "*" + antigravityPromptSpace + antigravityPromptInvisible + "*"
	return strings.Join(words, separator)
}
