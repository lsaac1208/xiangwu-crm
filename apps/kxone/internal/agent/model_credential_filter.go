package agent

import (
	"encoding/json"
	"html"
	"strings"
)

// Hold only a possible credential prefix across deltas. Ordinary text remains
// untouched; exact known credential representations are replaced before SSE.
type credentialStreamFilter struct {
	needles []string
	pending string
}

func newCredentialStreamFilter(key string) credentialStreamFilter {
	f := credentialStreamFilter{}
	if key == "" {
		return f
	}
	encoded, _ := json.Marshal(key)
	seen := map[string]bool{}
	for _, v := range []string{key, html.EscapeString(key), string(encoded[1 : len(encoded)-1])} {
		if v != "" && !seen[v] {
			f.needles = append(f.needles, v)
			seen[v] = true
		}
	}
	return f
}

func (f *credentialStreamFilter) Write(text string) string {
	combined := f.pending + text
	f.pending = ""
	for _, needle := range f.needles {
		combined = strings.ReplaceAll(combined, needle, "[凭证片段已隐藏]")
	}
	hold := 0
	for _, needle := range f.needles {
		n := len(needle) - 1
		if n > len(combined) {
			n = len(combined)
		}
		for ; n > hold; n-- {
			if strings.HasSuffix(combined, needle[:n]) {
				hold = n
				break
			}
		}
	}
	if hold > 0 {
		f.pending = combined[len(combined)-hold:]
		combined = combined[:len(combined)-hold]
	}
	return combined
}
func (f *credentialStreamFilter) Finish() string { value := f.pending; f.pending = ""; return value }
