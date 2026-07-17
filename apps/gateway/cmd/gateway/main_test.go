package main

import "testing"

func TestBoundedEnvInt(t *testing.T) {
	t.Setenv("COCOLA_TEST_BOUND", "200")
	value, err := boundedEnvInt("COCOLA_TEST_BOUND", 100, 1, 1000)
	if err != nil || value != 200 {
		t.Fatalf("bounded value = %d, %v; want 200", value, err)
	}

	t.Setenv("COCOLA_TEST_BOUND", "1001")
	if _, err := boundedEnvInt("COCOLA_TEST_BOUND", 100, 1, 1000); err == nil {
		t.Fatal("out-of-range value should fail")
	}
}
