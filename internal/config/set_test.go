package config

import "testing"

func TestSet(t *testing.T) {
	values := NewSet[string](3)
	if !values.Add("one") {
		t.Fatal("first addition should report a new value")
	}
	if values.Add("one") {
		t.Fatal("duplicate addition should not report a new value")
	}
	values.Add("two")

	if !values.Contains("one") || !values.Contains("two") {
		t.Errorf("set is missing an added value: %v", values)
	}
	if values.Contains("three") {
		t.Errorf("set unexpectedly contains three: %v", values)
	}
	if len(values) != 2 {
		t.Errorf("set length = %d, want 2: %v", len(values), values)
	}

	if !values.Delete("one") {
		t.Fatal("existing deletion should report a removed value")
	}
	if values.Delete("missing") {
		t.Fatal("missing deletion should not report a removed value")
	}
	if values.Contains("one") || !values.Contains("two") {
		t.Errorf("wrong values after deletion: %v", values)
	}
	if len(values) != 1 {
		t.Errorf("set length = %d after deletion, want 1: %v", len(values), values)
	}
}
