package service

import "testing"

func TestRelativeStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		want    string
		wantErr bool
	}{
		{name: "volume directory", target: "/var/lib/cocola/storage/pvc-a", want: "pvc-a"},
		{name: "nested directory", target: "/var/lib/cocola/storage/team/pvc-a", want: "team/pvc-a"},
		{name: "root", target: "/var/lib/cocola/storage", wantErr: true},
		{name: "sibling prefix", target: "/var/lib/cocola/storage-old/pvc-a", wantErr: true},
		{name: "parent", target: "/var/lib/cocola", wantErr: true},
		{name: "relative", target: "pvc-a", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relativeStoragePath("/var/lib/cocola/storage", tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("relativeStoragePath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("relativeStoragePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
