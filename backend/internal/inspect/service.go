package Inspect

import (
	Context "context"
	Errors "errors"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
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
