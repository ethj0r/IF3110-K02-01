package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Pgx "github.com/jackc/pgx/v5"
	Slog "log/slog"
	Os "os"
	Strconv "strconv"
	Strings "strings"
	Time "time"
)

const DefaultRows = int64(2000000)

func main() {
	if Err := Run(); Err != nil {
		Slog.Error("large dataset seeding failed", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	DSN, Source := ResolveDSN()
	if DSN == "" {
		return Errors.New("set SEED_LARGE_DATABASE_URL, TARGET_DATABASE_URL_3, or TARGET_DATABASE_URL")
	}
	Rows := DefaultRows
	if Raw := Strings.TrimSpace(Os.Getenv("SEED_LARGE_ROWS")); Raw != "" {
		Value, Err := Strconv.ParseInt(Raw, 10, 64)
		if Err != nil || Value < 1 {
			return Errors.New("SEED_LARGE_ROWS must be a positive integer")
		}
		Rows = Value
	}
	Force := Strings.EqualFold(Strings.TrimSpace(Os.Getenv("SEED_LARGE_FORCE")), "true")

	Cfg, Err := Pgx.ParseConfig(DSN)
	if Err != nil {
		return Errors.New("invalid seed database URL")
	}
	Cfg.ConnectTimeout = 5 * Time.Second
	Ctx, Cancel := Context.WithTimeout(Context.Background(), 30*Time.Minute)
	defer Cancel()
	Conn, Err := Pgx.ConnectConfig(Ctx, Cfg)
	if Err != nil {
		return Errors.New("seed database is unavailable")
	}
	defer Conn.Close(Context.Background())

	if _, Err := Conn.Exec(Ctx, "CREATE SCHEMA IF NOT EXISTS batch_demo"); Err != nil {
		return Errors.New("cannot create the batch_demo schema")
	}
	if _, Err := Conn.Exec(Ctx, `CREATE TABLE IF NOT EXISTS batch_demo.movies (
id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
title TEXT NOT NULL,
release_year INTEGER NOT NULL,
genre TEXT NOT NULL,
rating NUMERIC(3,1) NOT NULL,
views BIGINT NOT NULL DEFAULT 0
)`); Err != nil {
		return Errors.New("cannot create batch_demo.movies")
	}

	var Count int64
	if Err := Conn.QueryRow(Ctx, "SELECT count(*) FROM batch_demo.movies").Scan(&Count); Err != nil {
		return Errors.New("cannot count batch_demo.movies")
	}
	if Force && Count > 0 {
		if _, Err := Conn.Exec(Ctx, "TRUNCATE batch_demo.movies RESTART IDENTITY"); Err != nil {
			return Errors.New("cannot reset batch_demo.movies")
		}
		Count = 0
	}
	Inserted := int64(0)
	if Count == 0 {
		Started := Time.Now()
		Tag, Err := Conn.Exec(Ctx, `INSERT INTO batch_demo.movies (title, release_year, genre, rating, views)
SELECT
  (ARRAY['The','A','Last','First','Dark','Silent','Eternal','Hidden','Broken','Crimson'])[1 + (g % 10)::int] || ' ' ||
  (ARRAY['Journey','Empire','River','Shadow','Dream','City','Fire','Garden','Ocean','Night'])[1 + ((g / 10) % 10)::int] || ' ' ||
  (ARRAY['Part I','Part II','Returns','Rises','Awakens','Begins','Forever','Unbound','Reborn','Legacy'])[1 + ((g / 100) % 10)::int],
  1950 + (g % 75)::int,
  (ARRAY['Drama','Comedy','Action','Sci-Fi','Thriller','Romance','Horror','Fantasy','Animation','Documentary'])[1 + ((g / 7) % 10)::int],
  round((1 + (g % 90) / 10.0)::numeric, 1),
  0
FROM generate_series(1, $1::bigint) AS g`, Rows)
		if Err != nil {
			return Errors.New("cannot insert the large movie dataset")
		}
		Inserted = Tag.RowsAffected()
		Count = Inserted
		Fmt.Printf("Inserted %d movie row(s) into batch_demo.movies via %s in %s.\n", Inserted, Source, Time.Since(Started).Round(Time.Millisecond))
	} else {
		Fmt.Printf("batch_demo.movies already contains %d row(s); skipping insert (set SEED_LARGE_FORCE=true to reset).\n", Count)
	}

	Started := Time.Now()
	Tag, Err := Conn.Exec(Ctx, "UPDATE batch_demo.movies SET views = views + 1")
	if Err != nil {
		return Errors.New("cannot run the unbatched update")
	}
	Duration := Time.Since(Started)
	Fmt.Printf("Unbatched UPDATE touched %d movie row(s) in %s.\n", Tag.RowsAffected(), Duration.Round(Time.Millisecond))
	if Duration < Time.Second {
		Fmt.Println("The unbatched update finished under 1s. Increase SEED_LARGE_ROWS (and re-run with SEED_LARGE_FORCE=true) before using this dataset for batch comparison.")
	}
	return nil
}

func ResolveDSN() (string, string) {
	for _, Name := range []string{"SEED_LARGE_DATABASE_URL", "TARGET_DATABASE_URL_3", "TARGET_DATABASE_URL"} {
		if Value := Strings.TrimSpace(Os.Getenv(Name)); Value != "" {
			return Value, Name
		}
	}
	return "", ""
}
