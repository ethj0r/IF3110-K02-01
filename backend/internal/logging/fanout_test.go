package Logging

import (
	Bytes "bytes"
	Context "context"
	Slog "log/slog"
	Strings "strings"
	Testing "testing"
)

func TestExtraHandlersReceiveRedactedFilteredRecords(T *Testing.T) {
	Stdout := &Bytes.Buffer{}
	Shipped := &Bytes.Buffer{}
	Extra := Slog.NewJSONHandler(Shipped, &Slog.HandlerOptions{Level: Slog.LevelDebug})
	Logger := New(Stdout, Slog.LevelInfo, Extra, nil)
	Ctx := NewScope(Context.Background(), "request_id", "req-ship")
	Logger.DebugContext(Ctx, "hidden", "event", "debug.only")
	Logger.With("session_id", "abc").InfoContext(Ctx, "shipped", "event", "ship.test", "password", "hunter2",
		"target", "postgres://app:s3cret@db/app", Slog.Group("connection", "dsn", "postgres://x:y@h/d", "host", "db"))
	for Name, Output := range map[string]string{"stdout": Stdout.String(), "extra": Shipped.String()} {
		if Strings.Contains(Output, "debug.only") {
			T.Fatalf("%s received a record below LOG_LEVEL", Name)
		}
		for _, Secret := range []string{"hunter2", "s3cret", "x:y", `"session_id":"abc"`} {
			if Strings.Contains(Output, Secret) {
				T.Fatalf("%s leaked %q: %s", Name, Secret, Output)
			}
		}
		if !Strings.Contains(Output, `"request_id":"req-ship"`) || !Strings.Contains(Output, `"host":"db"`) {
			T.Fatalf("%s lost scope or group attributes: %s", Name, Output)
		}
	}
}
