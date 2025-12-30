package cloud

import "encoding/json"

// StringOrNumber accepts JSON values like 123 or "123" and stores them as a string.
type StringOrNumber string

func (s *StringOrNumber) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*s = ""
		return nil
	}

	// If it's a JSON string: "123"
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = StringOrNumber(str)
		return nil
	}

	// Otherwise assume it's a number: 123
	*s = StringOrNumber(string(b))
	return nil
}