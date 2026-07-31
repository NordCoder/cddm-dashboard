package orchestration

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestCountAmbiguousResultsPropagatesQueryFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := countAmbiguousResults(context.Background(), db, 1); err == nil {
		t.Fatal("closed database query was converted into a zero ambiguous count")
	}
}
