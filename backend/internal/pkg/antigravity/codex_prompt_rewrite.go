package antigravity

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

const antigravityPromptSpace = `[\s\v\p{Zs}\x{2028}\x{2029}\x{feff}]+`
const antigravityPromptInvisible = `[\x{00ad}\x{180e}\x{200b}-\x{200f}\x{2060}-\x{2064}\x{feff}]`

var (
	antigravityPromptInvisibleRe = regexp.MustCompile(antigravityPromptInvisible)
	antigravityPromptBasedOn     = regexp.MustCompile(`(?i)based(` + antigravityPromptSpace + `)on`)
	antigravityPromptYouAre      = regexp.MustCompile(`(?i)^(you)` + antigravityPromptSpace + `are`)
	codexPromptLines             = []codexPromptLine{
		{antigravityPromptLine("you are codex, an agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("you are codex, a coding agent based on gpt-5"), func(match string) string {
			return antigravityPromptBasedOn.ReplaceAllString(match, "built${1}on")
		}},
		{antigravityPromptLine("you are a coding agent running in the codex cli"), func(match string) string {
			return antigravityPromptYouAre.ReplaceAllString(match, "${1}'re")
		}},
	}
)

type codexPromptLine struct {
	pattern *regexp.Regexp
	rewrite func(string) string
}

// RewriteCodexPromptForAntigravity rewrites Codex identity lines that
// Antigravity rejects with a generic RESOURCE_EXHAUSTED response.
func RewriteCodexPromptForAntigravity(body []byte) []byte {
	if !bytes.Contains(bytes.ToLower(body), []byte("codex")) {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	request, ok := payload["request"].(map[string]any)
	if !ok {
		return body
	}
	changed := rewriteCodexTextParts(request["systemInstruction"])
	if contents, ok := request["contents"].([]any); ok {
		for _, item := range contents {
			content, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := content["role"].(string); role != "" && role != "user" {
				continue
			}
			changed = rewriteCodexTextParts(content) || changed
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

func rewriteCodexTextParts(value any) bool {
	content, ok := value.(map[string]any)
	if !ok {
		return false
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text, ok := part["text"].(string)
		if !ok {
			continue
		}
		if rewritten := rewriteCodexPromptText(text); rewritten != text {
			part["text"] = rewritten
			changed = true
		}
	}
	return changed
}

func rewriteCodexPromptText(text string) string {
	type match struct {
		start   int
		end     int
		rewrite func(string) string
	}
	var matches []match
	for _, line := range codexPromptLines {
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
	words := strings.Split(line, " ")
	for index, word := range words {
		characters := make([]string, 0, len(word))
		for _, character := range word {
			characters = append(characters, regexp.QuoteMeta(string(character)))
		}
		words[index] = strings.Join(characters, antigravityPromptInvisible+"*")
	}
	separator := antigravityPromptInvisible + "*" + antigravityPromptSpace + antigravityPromptInvisible + "*"
	return regexp.MustCompile("(?i)" + strings.Join(words, separator))
}
