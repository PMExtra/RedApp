package presets

import (
	"encoding/json"
	"fmt"
)

func (r *Retention) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != 2 || fields["enabled"] == nil || fields["keep_latest"] == nil {
		return fmt.Errorf("retention requires enabled and keep_latest")
	}
	var enabled *bool
	var keep *int
	if err := json.Unmarshal(fields["enabled"], &enabled); err != nil {
		return err
	}
	if err := json.Unmarshal(fields["keep_latest"], &keep); err != nil {
		return err
	}
	if enabled == nil || keep == nil {
		return fmt.Errorf("retention fields cannot be null")
	}
	*r = Retention{Enabled: *enabled, KeepLatest: *keep}
	return r.Validate()
}
