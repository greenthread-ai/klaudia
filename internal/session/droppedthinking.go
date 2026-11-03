package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
)

// ThinkingKey is what a transcript stores for a dropped thinking block: a
// hash of its signature (or redacted-thinking data). The signature itself is
// about 1.5 KB, and a resume after a system-prompt change drops every block
// in the history at once — 637 in one measured session, a megabyte on one
// line. 128 bits is ample to tell the blocks of one session apart.
func ThinkingKey(signature string) string {
	sum := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(sum[:16])
}

// DroppedThinking returns the ThinkingKey of every thinking block the
// transcript at path records as removed from the live history.
//
// A resume has to remove the same blocks. The API drops a mismatched block for
// one request and the loop then removes it, so every block written after that
// is bound to a history without it. The transcript still holds the block, and
// a resume that sends it changes the prefix of every later block: the API
// drops them all. Measured on one session: every resume dropped every block
// since its last compaction (371, 525, 619, 637), including those the
// previous resumed process had written and the API had accepted.
//
// Lines are decoded only when they name the subtype, so the extra pass costs
// a byte search over the file.
func DroppedThinking(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	marker := []byte(`"` + SubtypeThinkingDropped + `"`)
	var keys map[string]bool
	err = eachLine(f, func(line []byte) bool {
		if !bytes.Contains(line, marker) {
			return true
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil || e.Type != "system" || e.Subtype != SubtypeThinkingDropped {
			return true
		}
		for _, k := range e.Dropped {
			if keys == nil {
				keys = map[string]bool{}
			}
			keys[k] = true
		}
		return true
	})
	return keys, err
}
