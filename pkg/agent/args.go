package agent

import "encoding/json"

func unmarshalArgs(raw string, dst *map[string]any) error {
	if raw == "" {
		*dst = map[string]any{}
		return nil
	}
	return json.Unmarshal([]byte(raw), dst)
}
