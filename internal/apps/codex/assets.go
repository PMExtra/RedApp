package codex

import _ "embed"

//go:embed assets/openai-symbol.svg
var openAISymbol string

// OpenAISymbol is the user-supplied OpenAI brand mark, not a Codex-specific logo.
func OpenAISymbol() string { return openAISymbol }
