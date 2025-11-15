package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateSessionID generates a unique session ID
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// boolPtr returns a pointer to a boolean value
func boolPtr(b bool) *bool {
	return &b
}

// stringPtr returns a pointer to a string value
func stringPtr(s string) *string {
	return &s
}
