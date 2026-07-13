package objstore

import "testing"

func TestConfigValidateRequiresCompleteObjectStore(t *testing.T) {
	complete := Config{Endpoint: "minio:9000", AccessKey: "cocola", SecretKey: "secret", Bucket: "cocola"}
	if err := complete.Validate(); err != nil {
		t.Fatalf("complete config rejected: %v", err)
	}

	cases := []Config{
		{AccessKey: "cocola", SecretKey: "secret", Bucket: "cocola"},
		{Endpoint: "minio:9000", SecretKey: "secret", Bucket: "cocola"},
		{Endpoint: "minio:9000", AccessKey: "cocola", Bucket: "cocola"},
		{Endpoint: "minio:9000", AccessKey: "cocola", SecretKey: "secret"},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("incomplete config accepted: %+v", cfg)
		}
	}
}
