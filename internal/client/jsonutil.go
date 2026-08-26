package client

import "encoding/json"

// decodeUUIDList reads a JSON value that the documentation shows only as an
// empty array, so its element type is unknown. It accepts a list of strings, a
// list of objects carrying a uuid, or a bare string, and yields nil for
// anything else.
//
// Guessing wrong here would fail an otherwise valid read, so an unrecognised
// shape is dropped rather than treated as an error.
func decodeUUIDList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		return asStrings
	}

	var asObjects []struct {
		UUID     string `json:"uuid"`
		Resource string `json:"resource_uuid"`
		VM       string `json:"vm_uuid"`
	}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		var out []string
		for _, o := range asObjects {
			switch {
			case o.UUID != "":
				out = append(out, o.UUID)
			case o.Resource != "":
				out = append(out, o.Resource)
			case o.VM != "":
				out = append(out, o.VM)
			}
		}
		return out
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return []string{asString}
	}

	return nil
}
