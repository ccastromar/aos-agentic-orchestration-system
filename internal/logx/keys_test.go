package logx

import (
	"reflect"
	"testing"
)

// Parameters are logged by name, never by value: the values are the user's input.
func TestKeysLogsNamesNotValues(t *testing.T) {
	got := Keys(map[string]any{"customer_id": "C-7", "amount": 1200})
	if want := []string{"amount", "customer_id"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
	if Keys(map[string]string{}) == nil || len(Keys(map[string]string{})) != 0 {
		t.Fatal("an empty map has no keys")
	}
	if Keys("not a map") != nil {
		t.Fatal("a non-map has no keys")
	}
}
