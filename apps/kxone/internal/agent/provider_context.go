package agent

import "encoding/json"

func providerContextError(raw []byte) bool {
	var response struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return false
	}
	for _, code := range []string{response.Error.Code, response.Error.Type} {
		switch code {
		case "context_length_exceeded", "context_window_exceeded", "context_exceeded_error", "max_context_length":
			return true
		}
	}
	return false
}
