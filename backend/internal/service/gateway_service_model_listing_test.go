package service

import "testing"

func TestCollapseAntigravityModelForListing(t *testing.T) {
	tests := []struct {
		platform string
		model    string
		want     string
	}{
		{PlatformAntigravity, "claude-opus-5-5-low", "claude-opus-5-5"},
		{PlatformAntigravity, "claude-opus-5-5-medium", "claude-opus-5-5"},
		{PlatformAntigravity, "claude-opus-5-5-high", "claude-opus-5-5"},
		{PlatformAntigravity, "claude-opus-5-5", "claude-opus-5-5"},
		{PlatformAntigravity, "gemini-4-argon-low", "gemini-4-argon"},
		{PlatformAntigravity, "gemini-4-argon-medium", "gemini-4-argon"},
		{PlatformAntigravity, "gemini-4-argon-high", "gemini-4-argon"},
		{PlatformGemini, "gemini-4-argon-high", "gemini-4-argon"},
		{PlatformAntigravity, "claude-sonnet-5-5-high", "claude-sonnet-5-5-high"},
		{PlatformAnthropic, "claude-opus-5-5-high", "claude-opus-5-5-high"},
	}
	for _, tt := range tests {
		t.Run(tt.platform+"/"+tt.model, func(t *testing.T) {
			if got := collapseAntigravityModelForListing(tt.platform, tt.model); got != tt.want {
				t.Fatalf("collapseAntigravityModelForListing(%q, %q) = %q, want %q", tt.platform, tt.model, got, tt.want)
			}
		})
	}
}
