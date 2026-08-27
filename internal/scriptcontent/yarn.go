package scriptcontent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	YarnContentFormat        = "yarn"
	YarnContentSchema        = "new-yokosuka-yarn-v1"
	YarnCompilerVersion      = "yarnspinner-3.2.1"
	YarnCommandSchemaVersion = "new-yokosuka-commands-v1"
	MaxYarnSourceBytes       = 512 * 1024
	// Yarn numbers are IEEE-754 doubles. Integer-valued commands and queries
	// stay inside this range so every value survives the runtime exactly.
	YarnMaxSafeInteger int64 = 1<<53 - 1
)

// CanonicalYarnSource normalizes transport-specific newlines without
// rewriting author formatting or attempting to parse Yarn syntax.
func CanonicalYarnSource(source string) (string, error) {
	if len(source) > MaxYarnSourceBytes {
		return "", errors.New("Yarn source exceeds 512 KiB")
	}
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	if strings.TrimSpace(source) == "" {
		return "", errors.New("Yarn source is required")
	}
	if !strings.HasSuffix(source, "\n") {
		source += "\n"
	}
	return source, nil
}

func SourceHash(source string) string {
	return BytesHash([]byte(source))
}

func BytesHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
