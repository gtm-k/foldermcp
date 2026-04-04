package sandbox

import (
	"fmt"
	"regexp"
)

// secretPatterns matches common secret/credential formats that should be
// redacted from subprocess output.
var secretPatterns = []*regexp.Regexp{
	// AWS access key IDs (always start with AKIA)
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// AWS secret access keys (40 chars of base64-ish)
	regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_key)\s*[:=]\s*[A-Za-z0-9/+=]{40}`),
	// Generic API key patterns in key=value or key: value
	regexp.MustCompile(`(?i)(?:api[_-]?key|apikey)\s*[:=]\s*\S{10,}`),
	// GitHub personal access tokens
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	// GitHub fine-grained tokens
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	// OpenAI / Anthropic keys (sk-...)
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	// Private key headers
	regexp.MustCompile(`-----BEGIN\s+\S*\s*PRIVATE KEY-----`),
}

// longTokenPattern matches suspiciously long alphanumeric strings (40+ chars)
// that may be tokens or secrets. Used in both Sanitize and SanitizeParams.
var longTokenPattern = regexp.MustCompile(`[A-Za-z0-9_-]{40,}`)

// Sanitizer redacts secrets and truncates oversized output.
type Sanitizer struct {
	maxBytes int
}

// NewSanitizer creates a Sanitizer. If maxBytes is 0, no truncation is applied.
// Default when called from NewExecutor: 100KB.
func NewSanitizer(maxBytes int) *Sanitizer {
	if maxBytes < 0 {
		maxBytes = 100 * 1024
	}
	return &Sanitizer{maxBytes: maxBytes}
}

// Sanitize redacts secrets and truncates output if needed.
func (s *Sanitizer) Sanitize(output string) string {
	// 1. Redact known secret patterns.
	result := redactSecrets(output)

	// 2. Redact long token-like strings (40+ alphanumeric chars).
	result = longTokenPattern.ReplaceAllString(result, "[REDACTED]")

	// 3. Truncate if over limit.
	if s.maxBytes > 0 && len(result) > s.maxBytes {
		result = result[:s.maxBytes] + fmt.Sprintf("... [output truncated at %d bytes]", s.maxBytes)
	}

	return result
}

// SanitizeParams redacts secrets from parameter strings before logging.
// In addition to standard secret patterns, it also redacts long token-like
// strings (40+ alphanumeric characters).
func (s *Sanitizer) SanitizeParams(params string) string {
	result := redactSecrets(params)
	result = longTokenPattern.ReplaceAllString(result, "[REDACTED]")
	return result
}

// redactSecrets applies all secret patterns to the input string.
func redactSecrets(input string) string {
	result := input
	for _, pat := range secretPatterns {
		result = pat.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}
