package Inspect

import (
	Context "context"
	Errors "errors"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
	Math "math"
	Strings "strings"
	Time "time"
)

type Connector interface {
	DialDSN(Ctx Context.Context, ID string) (Connection.Connection, string, error)
}

type Service struct {
	Connections Connector
	Driver      Driver.Driver
}

func (Server *Service) openTarget(Ctx Context.Context, DBID string) (Driver.Conn, error) {
	if DBID == "" {
		return nil, ErrNotFound
	}
	_, DSN, Err := Server.Connections.DialDSN(Ctx, DBID)
	if Errors.Is(Err, Connection.ErrNotFound) {
		return nil, ErrNotFound
	}
	if Err != nil {
		return nil, ErrUnavailable
	}
	Target, Err := Server.Driver.Open(DSN)
	if Err != nil {
		return nil, ErrUnavailable
	}
	return Target, nil
}

func (Server *Service) Schemas(Ctx Context.Context, DBID string) ([]string, error) {
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return nil, Err
	}
	defer Target.Close()
	Rows, Err := Target.Query(Ctx, `SELECT S.schema_name FROM information_schema.schemata S
WHERE S.schema_name NOT IN ('pg_catalog', 'information_schema') AND S.schema_name NOT LIKE 'pg_toast%' AND S.schema_name NOT LIKE 'pg_temp_%'
ORDER BY S.schema_name`)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []string{}
	for Rows.Next() {
		var Name string
		if Err := Rows.Scan(&Name); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Name)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func checkTableRef(Schema, Table string) error {
	if Schema == "" {
		Schema = "public"
	}
	if Err := Driver.CheckIdentifier(Schema); Err != nil {
		return ErrInvalidInput
	}
	if Err := Driver.CheckIdentifier(Table); Err != nil {
		return ErrInvalidInput
	}
	return nil
}

func (Server *Service) Tables(Ctx Context.Context, DBID, Schema string) ([]Table, error) {
	if Schema == "" {
		Schema = "public"
	}
	if Err := Driver.CheckIdentifier(Schema); Err != nil {
		return nil, ErrInvalidInput
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return nil, Err
	}
	defer Target.Close()
	Rows, Err := Target.Query(Ctx, `SELECT T.table_name, T.table_type FROM information_schema.tables T
WHERE T.table_schema = $1 AND T.table_type IN ('BASE TABLE', 'VIEW') ORDER BY T.table_name`, Schema)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Table{}
	for Rows.Next() {
		var Item Table
		if Err := Rows.Scan(&Item.Name, &Item.Type); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) Columns(Ctx Context.Context, DBID, Schema, Table string) ([]Column, error) {
	if Schema == "" {
		Schema = "public"
	}
	if Err := checkTableRef(Schema, Table); Err != nil {
		return nil, Err
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return nil, Err
	}
	defer Target.Close()
	Rows, Err := Target.Query(Ctx, `SELECT C.column_name, pg_catalog.format_type(A.atttypid, A.atttypmod), C.is_nullable = 'YES', C.column_default
FROM information_schema.columns C
JOIN pg_catalog.pg_namespace N ON N.nspname = C.table_schema
JOIN pg_catalog.pg_class T ON T.relnamespace = N.oid AND T.relname = C.table_name
JOIN pg_catalog.pg_attribute A ON A.attrelid = T.oid AND A.attnum = C.ordinal_position
WHERE C.table_schema = $1 AND C.table_name = $2 ORDER BY C.ordinal_position`, Schema, Table)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Column{}
	for Rows.Next() {
		var Item Column
		if Err := Rows.Scan(&Item.Name, &Item.DataType, &Item.Nullable, &Item.Default); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) Constraints(Ctx Context.Context, DBID, Schema, Table string) ([]Constraint, error) {
	if Schema == "" {
		Schema = "public"
	}
	if Err := checkTableRef(Schema, Table); Err != nil {
		return nil, Err
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return nil, Err
	}
	defer Target.Close()
	Rows, Err := Target.Query(Ctx, `SELECT C.conname,
CASE C.contype WHEN 'p' THEN 'PRIMARY KEY' WHEN 'u' THEN 'UNIQUE'
WHEN 'f' THEN 'FOREIGN KEY' WHEN 'c' THEN 'CHECK' WHEN 'x' THEN 'EXCLUDE'
WHEN 'n' THEN 'NOT NULL' ELSE C.contype::text END,
COALESCE(string_agg(A.attname, ', ' ORDER BY K.ordinality), ''), pg_catalog.pg_get_constraintdef(C.oid, true)
FROM pg_catalog.pg_constraint C
JOIN pg_catalog.pg_class T ON T.oid = C.conrelid
JOIN pg_catalog.pg_namespace N ON N.oid = T.relnamespace
LEFT JOIN LATERAL unnest(C.conkey) WITH ORDINALITY K(attnum, ordinality) ON true
LEFT JOIN pg_catalog.pg_attribute A ON A.attrelid = T.oid AND A.attnum = K.attnum
WHERE N.nspname = $1 AND T.relname = $2
AND has_schema_privilege(N.oid, 'USAGE') AND has_table_privilege(T.oid, 'SELECT')
GROUP BY C.oid, C.conname, C.contype ORDER BY C.conname`, Schema, Table)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Constraint{}
	for Rows.Next() {
		var Item Constraint
		if Err := Rows.Scan(&Item.Name, &Item.Type, &Item.Columns, &Item.Definition); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) Indexes(Ctx Context.Context, DBID, Schema, Table string) ([]Index, error) {
	if Schema == "" {
		Schema = "public"
	}
	if Err := checkTableRef(Schema, Table); Err != nil {
		return nil, Err
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return nil, Err
	}
	defer Target.Close()
	Rows, Err := Target.Query(Ctx, `SELECT I.indexname, I.indexdef FROM pg_indexes I
WHERE I.schemaname = $1 AND I.tablename = $2 ORDER BY I.indexname`, Schema, Table)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Index{}
	for Rows.Next() {
		var Item Index
		if Err := Rows.Scan(&Item.Name, &Item.Definition); Err != nil {
			return nil, ErrUnavailable
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Server *Service) tableExists(Ctx Context.Context, Target Driver.Tx, Schema, Table string) (bool, error) {
	Rows, Err := Target.Query(Ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables T
WHERE T.table_schema = $1 AND T.table_name = $2)`, Schema, Table)
	if Err != nil {
		return false, ErrUnavailable
	}
	defer Rows.Close()
	var Exists bool
	if Rows.Next() {
		if Err := Rows.Scan(&Exists); Err != nil {
			return false, ErrUnavailable
		}
	}
	if Err := Rows.Err(); Err != nil {
		return false, ErrUnavailable
	}
	return Exists, nil
}

func tableOrder(Ctx Context.Context, Target Driver.Tx, Schema, Table string) (string, error) {
	Rows, Err := Target.Query(Ctx, `SELECT A.attname FROM pg_catalog.pg_index I
JOIN pg_catalog.pg_class T ON T.oid = I.indrelid
JOIN pg_catalog.pg_namespace N ON N.oid = T.relnamespace
JOIN LATERAL unnest(I.indkey) WITH ORDINALITY K(attnum, ordinality) ON K.ordinality <= I.indnkeyatts
JOIN pg_catalog.pg_attribute A ON A.attrelid = T.oid AND A.attnum = K.attnum
WHERE N.nspname = $1 AND T.relname = $2 AND I.indisprimary ORDER BY K.ordinality`, Schema, Table)
	if Err != nil {
		return "", ErrUnavailable
	}
	defer Rows.Close()
	Columns := []string{}
	for Rows.Next() {
		var Name string
		if Err := Rows.Scan(&Name); Err != nil {
			return "", ErrUnavailable
		}
		Columns = append(Columns, Pgx.Identifier{"browse_row", Name}.Sanitize())
	}
	if Rows.Err() != nil {
		return "", ErrUnavailable
	}
	Fallback := `ROW(browse_row.*)::text COLLATE "C"`
	if len(Columns) == 0 {
		return Fallback, nil
	}
	return Strings.Join(Columns, ", ") + ", " + Fallback, nil
}

func (Server *Service) Rows(Ctx Context.Context, DBID, Schema, Table string, Page, PageSize int) (TablePage, error) {
	Ctx, Cancel := Context.WithTimeout(Ctx, 10*Time.Second)
	defer Cancel()
	if Schema == "" {
		Schema = "public"
	}
	if Err := checkTableRef(Schema, Table); Err != nil {
		return TablePage{}, Err
	}
	if Page < 1 || PageSize < 1 || PageSize > 100 || Page-1 > Math.MaxInt/PageSize {
		return TablePage{}, ErrInvalidInput
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return TablePage{}, Err
	}
	defer Target.Close()
	Snapshot, Err := Target.Begin(Ctx)
	if Err != nil {
		return TablePage{}, ErrUnavailable
	}
	defer func() {
		Cleanup, Cancel := Context.WithTimeout(Context.Background(), 5*Time.Second)
		defer Cancel()
		_ = Snapshot.Rollback(Cleanup)
	}()
	if Err := Snapshot.Exec(Ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); Err != nil {
		return TablePage{}, ErrUnavailable
	}
	Exists, Err := Server.tableExists(Ctx, Snapshot, Schema, Table)
	if Err != nil {
		return TablePage{}, Err
	}
	if !Exists {
		return TablePage{}, ErrNotFound
	}
	Count, Err := Snapshot.Query(Ctx, "SELECT COUNT(*) FROM "+Pgx.Identifier{Schema, Table}.Sanitize())
	if Err != nil {
		return TablePage{}, ErrUnavailable
	}
	var Total int
	for Count.Next() {
		if Err := Count.Scan(&Total); Err != nil {
			Count.Close()
			return TablePage{}, ErrUnavailable
		}
	}
	Count.Close()
	if Err := Count.Err(); Err != nil {
		return TablePage{}, ErrUnavailable
	}
	Order, Err := tableOrder(Ctx, Snapshot, Schema, Table)
	if Err != nil {
		return TablePage{}, Err
	}
	Data, Err := Snapshot.Query(Ctx, "SELECT browse_row.* FROM "+Pgx.Identifier{Schema, Table}.Sanitize()+" AS browse_row ORDER BY "+Order+" LIMIT $1 OFFSET $2", Pgx.QueryResultFormats{Pgx.TextFormatCode}, PageSize, (Page-1)*PageSize)
	if Err != nil {
		return TablePage{}, ErrUnavailable
	}
	defer Data.Close()
	Fields := []string{}
	for _, Field := range Data.FieldDescriptions() {
		Fields = append(Fields, Field.Name)
	}
	Result := TablePage{Columns: Fields, Rows: [][]*string{}, Page: Page, PageSize: PageSize, Total: Total}
	for Data.Next() {
		// PostgreSQL text preserves numeric precision and supports every target type.
		Values := make([]*string, len(Fields))
		for Index, Raw := range Data.RawValues() {
			if Raw != nil {
				Value := string(Raw)
				Values[Index] = &Value
			}
		}
		Result.Rows = append(Result.Rows, Values)
	}
	if Err := Data.Err(); Err != nil {
		return TablePage{}, ErrUnavailable
	}
	return Result, nil
}
