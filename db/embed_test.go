package db

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedSQLMigrationsHaveGooseSections(t *testing.T) {
	files, err := fs.Glob(Migrations, MigrationsDir+"/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded SQL migrations")
	}
	for _, name := range files {
		raw, err := Migrations.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(raw)
		up := strings.Index(text, "-- +goose Up")
		down := strings.Index(text, "-- +goose Down")
		if up < 0 || down < 0 || up > down {
			t.Errorf("%s must contain ordered goose Up and Down sections", name)
		}
		begins := strings.Count(text, "-- +goose StatementBegin")
		ends := strings.Count(text, "-- +goose StatementEnd")
		if begins != ends {
			t.Errorf("%s has %d StatementBegin markers and %d StatementEnd markers", name, begins, ends)
		}
	}
}
