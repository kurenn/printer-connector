package cloud

import (
	"encoding/json"
	"testing"
)

func TestStringOrNumber_Unmarshal(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"json number": {`123`, "123"},
		"json string": {`"abc"`, "abc"},
		"numeric str": {`"456"`, "456"},
		"null":        {`null`, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var s StringOrNumber
			if err := json.Unmarshal([]byte(c.in), &s); err != nil {
				t.Fatalf("Unmarshal(%s): %v", c.in, err)
			}
			if s.String() != c.want {
				t.Errorf("Unmarshal(%s) = %q, want %q", c.in, s.String(), c.want)
			}
		})
	}
}

// Commands and webcam requests arrive with the ID as either 123 or "123";
// both must decode into the same struct field.
func TestStringOrNumber_InStruct(t *testing.T) {
	var c Command
	if err := json.Unmarshal([]byte(`{"id": 7, "printer_id": 1, "action": "pause"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID.String() != "7" {
		t.Errorf("numeric id = %q, want \"7\"", c.ID.String())
	}
	if err := json.Unmarshal([]byte(`{"id": "8", "printer_id": 1, "action": "pause"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.ID.String() != "8" {
		t.Errorf("string id = %q, want \"8\"", c.ID.String())
	}
}
