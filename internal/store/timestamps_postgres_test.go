package store_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPostgresTimestampsAreUTC(t *testing.T) {
	// Isolate time.Local from other tests and cover hosts outside UTC even when
	// CI itself runs in UTC. The PostgreSQL session uses a different offset.
	if os.Getenv("HIKYO_TEST_UTC_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPostgresTimestampsAreUTC$", "-test.v")
		cmd.Env = append(os.Environ(), "TZ=Pacific/Auckland", "HIKYO_TEST_UTC_CHILD=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("non-UTC subprocess: %v\n%s", err, out)
		}
		t.Logf("%s", out)
		return
	}
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Fatal("regression requires a non-UTC process timezone")
	}
	db := postgresTestDB(t)
	ctx := t.Context()
	// Hold both connections so the second acquire must open another physical
	// connection and prove normalization is installed beyond the initial ping.
	first, err := db.PG().Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := db.PG().Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	for i, conn := range []*pgx.Conn{first.Conn(), second.Conn()} {
		if _, err := conn.Exec(ctx, "SET TIME ZONE 'Asia/Kathmandu'"); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeCacheStatement, pgx.QueryExecModeSimpleProtocol} {
			for _, input := range []string{"2026-09-05T01:04:56.123456+05:45", "2026-01-05T23:34:56.654321-07:00"} {
				t.Run(mode.String()+"/"+input, func(t *testing.T) {
					want, err := time.Parse(time.RFC3339Nano, input)
					if err != nil {
						t.Fatal(err)
					}
					var direct time.Time
					var wrapped, null pgtype.Timestamptz
					var array []pgtype.Timestamptz
					err = conn.QueryRow(ctx, `SELECT $1::timestamptz, $1::timestamptz,
						NULL::timestamptz, ARRAY[$1::timestamptz, NULL::timestamptz]`, mode, input).
						Scan(&direct, &wrapped, &null, &array)
					if err != nil {
						t.Fatal(err)
					}
					if !wrapped.Valid || null.Valid || len(array) != 2 || !array[0].Valid || array[1].Valid {
						t.Fatalf("connection %d: invalid timestamp/null decoding: %+v %+v %+v", i, wrapped, null, array)
					}
					for name, got := range map[string]time.Time{"direct": direct, "generated query wrapper": wrapped.Time, "array": array[0].Time} {
						if !got.Equal(want) {
							t.Errorf("connection %d %s changed instant: got %s, want %s", i, name, got, want)
						}
						if got.Location() != time.UTC {
							t.Errorf("connection %d %s location = %s, want UTC", i, name, got.Location())
						}
						wire, err := json.Marshal(got)
						if err != nil || !strings.HasSuffix(string(wire), `Z"`) {
							t.Errorf("connection %d %s JSON = %s, error %v; want canonical Z timestamp", i, name, wire, err)
						}
					}
				})
			}
		}
	}
}
