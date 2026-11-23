package main

import (
	Context "context"
	Errors "errors"
	Fmt "fmt"
	Logging "github.com/ethj0r/IF3110-K02-01/backend/internal/logging"
	Pgx "github.com/jackc/pgx/v5"
	Slog "log/slog"
	Os "os"
	Strings "strings"
	Time "time"
)

type dataset struct {
	Label  string
	Env    string
	DDL    string
	Insert string
}

func main() {
	Slog.SetDefault(Logging.New(Os.Stderr, Logging.ParseLevel(Os.Getenv("LOG_LEVEL"))))
	if Err := Run(); Err != nil {
		Slog.Error("sample dataset seeding failed", "error", Err)
		Os.Exit(1)
	}
}

func Run() error {
	Datasets := []dataset{MovieDataset, BookDataset}
	Seeded := 0
	var Failures []error
	for _, Item := range Datasets {
		DSN := Strings.TrimSpace(Os.Getenv(Item.Env))
		if DSN == "" {
			continue
		}
		if Err := Seed(Context.Background(), DSN, Item); Err != nil {
			Failures = append(Failures, Err)
			continue
		}
		Seeded++
		Fmt.Printf("Seeded %s via %s.\n", Item.Label, Item.Env)
	}
	if Seeded == 0 && len(Failures) == 0 {
		return Errors.New("set TARGET_DATABASE_URL and/or TARGET_DATABASE_URL_2")
	}
	return Errors.Join(Failures...)
}

func Seed(Parent Context.Context, DSN string, Item dataset) error {
	Cfg, Err := Pgx.ParseConfig(DSN)
	if Err != nil {
		return Fmt.Errorf("invalid database URL for %s", Item.Label)
	}
	Cfg.ConnectTimeout = 5 * Time.Second
	Cfg.Tracer = Logging.SQLTracer{Database: "target"}
	Ctx, Cancel := Context.WithTimeout(Parent, 2*Time.Minute)
	defer Cancel()
	Conn, Err := Pgx.ConnectConfig(Ctx, Cfg)
	if Err != nil {
		return Fmt.Errorf("%s database is unavailable", Item.Label)
	}
	defer Conn.Close(Context.Background())
	if _, Err := Conn.Exec(Ctx, "CREATE SCHEMA IF NOT EXISTS demo"); Err != nil {
		return Fmt.Errorf("cannot create the demo schema for %s", Item.Label)
	}
	if _, Err := Conn.Exec(Ctx, Item.DDL); Err != nil {
		return Fmt.Errorf("cannot create the %s table", Item.Label)
	}
	if _, Err := Conn.Exec(Ctx, Item.Insert); Err != nil {
		return Fmt.Errorf("cannot insert the %s rows", Item.Label)
	}
	return nil
}

var MovieDataset = dataset{
	Label: "demo.movies",
	Env:   "TARGET_DATABASE_URL",
	DDL: `CREATE TABLE IF NOT EXISTS demo.movies (
id INTEGER PRIMARY KEY,
title TEXT NOT NULL,
release_year INTEGER NOT NULL,
genre TEXT NOT NULL,
rating NUMERIC(3,1) NOT NULL,
views BIGINT NOT NULL DEFAULT 0
)`,
	Insert: `INSERT INTO demo.movies (id, title, release_year, genre, rating, views) VALUES
(1, 'The Shawshank Redemption', 1994, 'Drama', 9.3, 0),
(2, 'The Godfather', 1972, 'Crime', 9.2, 0),
(3, 'The Dark Knight', 2008, 'Action', 9.0, 0),
(4, 'Pulp Fiction', 1994, 'Crime', 8.9, 0),
(5, 'Schindler''s List', 1993, 'History', 9.0, 0),
(6, 'The Lord of the Rings: The Return of the King', 2003, 'Fantasy', 9.0, 0),
(7, 'The Good, the Bad and the Ugly', 1966, 'Western', 8.8, 0),
(8, 'Fight Club', 1999, 'Drama', 8.8, 0),
(9, 'Forrest Gump', 1994, 'Drama', 8.8, 0),
(10, 'Inception', 2010, 'Sci-Fi', 8.8, 0),
(11, 'The Matrix', 1999, 'Sci-Fi', 8.7, 0),
(12, 'Goodfellas', 1990, 'Crime', 8.7, 0)
ON CONFLICT (id) DO NOTHING`,
}

var BookDataset = dataset{
	Label: "demo.books",
	Env:   "TARGET_DATABASE_URL_2",
	DDL: `CREATE TABLE IF NOT EXISTS demo.books (
id INTEGER PRIMARY KEY,
title TEXT NOT NULL,
author TEXT NOT NULL,
published_year INTEGER NOT NULL,
genre TEXT NOT NULL,
rating NUMERIC(2,1) NOT NULL
)`,
	Insert: `INSERT INTO demo.books (id, title, author, published_year, genre, rating) VALUES
(1, 'Pride and Prejudice', 'Jane Austen', 1813, 'Classic', 4.3),
(2, '1984', 'George Orwell', 1949, 'Dystopian', 4.7),
(3, 'To Kill a Mockingbird', 'Harper Lee', 1960, 'Classic', 4.6),
(4, 'The Great Gatsby', 'F. Scott Fitzgerald', 1925, 'Classic', 4.4),
(5, 'Moby-Dick', 'Herman Melville', 1851, 'Adventure', 4.1),
(6, 'War and Peace', 'Leo Tolstoy', 1869, 'Historical', 4.5),
(7, 'Crime and Punishment', 'Fyodor Dostoevsky', 1866, 'Psychological', 4.5),
(8, 'The Hobbit', 'J.R.R. Tolkien', 1937, 'Fantasy', 4.6),
(9, 'Dune', 'Frank Herbert', 1965, 'Sci-Fi', 4.5),
(10, 'The Left Hand of Darkness', 'Ursula K. Le Guin', 1969, 'Sci-Fi', 4.4),
(11, 'One Hundred Years of Solitude', 'Gabriel Garcia Marquez', 1967, 'Magical Realism', 4.6),
(12, 'Things Fall Apart', 'Chinua Achebe', 1958, 'Historical', 4.4)
ON CONFLICT (id) DO NOTHING`,
}
