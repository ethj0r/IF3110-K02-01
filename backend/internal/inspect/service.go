package Inspect

import (
	Context "context"
	Errors "errors"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
	Pgx "github.com/jackc/pgx/v5"
)

type Connector interface {
	List(Ctx Context.Context) ([]Connection.Connection, error)
	DialDSN(Ctx Context.Context, ID string) (Connection.Connection, string, error)
}

type Service struct {
	Connections Connector
	Driver      Driver.Driver
}

func (Server *Service) ListDatabases(Ctx Context.Context) ([]Database, error) {
	Items, Err := Server.Connections.List(Ctx)
	if Err != nil {
		return nil, ErrUnavailable
	}
	Result := make([]Database, 0, len(Items))
	for _, Item := range Items {
		Result = append(Result, Database{ID: Item.ID, Name: Item.Name})
	}
	return Result, nil
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
	Rows, Err := Target.Query(Ctx, `SELECT C.column_name, C.data_type, C.is_nullable = 'YES', C.column_default
FROM information_schema.columns C WHERE C.table_schema = $1 AND C.table_name = $2 ORDER BY C.ordinal_position`, Schema, Table)
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
	Rows, Err := Target.Query(Ctx, `SELECT C.constraint_name, C.constraint_type,
COALESCE(string_agg(K.column_name, ', ' ORDER BY K.ordinal_position), '') AS columns
FROM information_schema.table_constraints C LEFT JOIN information_schema.key_column_usage K
ON K.constraint_name = C.constraint_name AND K.constraint_schema = C.constraint_schema AND K.table_name = C.table_name
WHERE C.table_schema = $1 AND C.table_name = $2
GROUP BY C.constraint_name, C.constraint_type ORDER BY C.constraint_name`, Schema, Table)
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Constraint{}
	for Rows.Next() {
		var Item Constraint
		if Err := Rows.Scan(&Item.Name, &Item.Type, &Item.Columns); Err != nil {
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

func (Server *Service) tableExists(Ctx Context.Context, Target Driver.Conn, Schema, Table string) (bool, error) {
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

func (Server *Service) Rows(Ctx Context.Context, DBID, Schema, Table string, Page, PageSize int) (TablePage, error) {
	if Schema == "" {
		Schema = "public"
	}
	if Err := checkTableRef(Schema, Table); Err != nil {
		return TablePage{}, Err
	}
	if Page < 1 || PageSize < 1 || PageSize > 100 {
		return TablePage{}, ErrInvalidInput
	}
	Target, Err := Server.openTarget(Ctx, DBID)
	if Err != nil {
		return TablePage{}, Err
	}
	defer Target.Close()
	Exists, Err := Server.tableExists(Ctx, Target, Schema, Table)
	if Err != nil {
		return TablePage{}, Err
	}
	if !Exists {
		return TablePage{}, ErrNotFound
	}
	Count, Err := Target.Query(Ctx, "SELECT COUNT(*) FROM "+Pgx.Identifier{Schema, Table}.Sanitize())
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
	Data, Err := Target.Query(Ctx, "SELECT * FROM "+Pgx.Identifier{Schema, Table}.Sanitize()+" LIMIT $1 OFFSET $2", PageSize, (Page-1)*PageSize)
	if Err != nil {
		return TablePage{}, ErrUnavailable
	}
	defer Data.Close()
	Fields := []string{}
	for _, Field := range Data.FieldDescriptions() {
		Fields = append(Fields, Field.Name)
	}
	Result := TablePage{Columns: Fields, Rows: [][]any{}, Page: Page, PageSize: PageSize, Total: Total}
	for Data.Next() {
		Values, Err := Data.Values()
		if Err != nil {
			return TablePage{}, ErrUnavailable
		}
		Result.Rows = append(Result.Rows, Values)
	}
	if Err := Data.Err(); Err != nil {
		return TablePage{}, ErrUnavailable
	}
	return Result, nil
}
