package gocode

// Usage records token consumption for one API call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

const defaultMaxTokens = 1024
