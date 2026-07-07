package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cocola-project/cocola/apps/admin-api/internal/store"
)

func TestTokenUsageReportDefaults(t *testing.T) {
	svc := New(store.NewMemory(), nil, authTestClock)
	report, err := svc.TokenUsageReport(context.Background(), store.TokenUsageQuery{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.Bucket != "day" {
		t.Fatalf("default bucket = %q, want day", report.Bucket)
	}
	if !report.To.Equal(authTestClock()) {
		t.Fatalf("default to = %s, want %s", report.To, authTestClock())
	}
	if got, want := report.To.Sub(report.From), 30*24*time.Hour; got != want {
		t.Fatalf("default range = %s, want %s", got, want)
	}
	if report.Summary.TotalTokens != 0 || len(report.Trend) != 0 || len(report.Users) != 0 {
		t.Fatalf("memory backend should return empty usage: %+v", report)
	}
}

func TestExportTokenUsageXLSX(t *testing.T) {
	svc := New(store.NewMemory(), nil, authTestClock)
	data, name, err := svc.ExportTokenUsageXLSX(context.Background(), store.TokenUsageQuery{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if name == "" || len(data) == 0 {
		t.Fatalf("export returned empty name/data")
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("xlsx should be a zip container, got prefix %q", data[:2])
	}
}
