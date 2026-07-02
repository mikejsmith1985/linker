package claude

// Compile-time assertions that both implementations satisfy LLM.
var _ LLM = (*Client)(nil)
var _ LLM = (*Fake)(nil)
