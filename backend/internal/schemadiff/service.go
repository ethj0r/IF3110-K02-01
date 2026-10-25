package Schemadiff

import (
	Context "context"
	Errors "errors"
	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Connection "github.com/ethj0r/IF3110-K02-01/backend/internal/connection"
	Strings "strings"
	Time "time"
)

type Resolver interface {
	DialDSN(Context.Context, string) (Connection.Connection, string, error)
}

type Generator interface {
	Generate(Context.Context, string, string, string) (Result, error)
}

type Service struct {
	Policy      Access.Evaluator
	Connections Resolver
	Generator   Generator
}

func (Server *Service) Compare(Ctx Context.Context, User Auth.Principal, DatabaseID string, Value Input) (Result, error) {
	Ctx, Cancel := Context.WithTimeout(Ctx, 20*Time.Second)
	defer Cancel()
	Value = Value.Normalized()
	DatabaseID = Strings.TrimSpace(DatabaseID)
	if DatabaseID == "" || len(DatabaseID) > 64 {
		return Result{}, ErrInvalidInput
	}
	if Err := Value.Validate(); Err != nil {
		return Result{}, Err
	}
	for _, Permission := range []struct {
		ID     string
		Action Access.Action
	}{{DatabaseID, Access.ActionEdit}, {Value.ReferenceDatabaseID, Access.ActionView}} {
		Allowed, Err := Access.Can(Ctx, Server.Policy, User, Permission.Action, Permission.ID)
		if Err != nil {
			return Result{}, ErrUnavailable
		}
		if !Allowed {
			return Result{}, ErrForbidden
		}
	}
	if Server.Connections == nil || Server.Generator == nil {
		return Result{}, ErrUnavailable
	}
	CurrentDSN, Err := Server.Resolve(Ctx, DatabaseID)
	if Err != nil {
		return Result{}, Err
	}
	ReferenceDSN, Err := Server.Resolve(Ctx, Value.ReferenceDatabaseID)
	if Err != nil {
		return Result{}, Err
	}
	Generated, Err := Server.Generator.Generate(Ctx, CurrentDSN, ReferenceDSN, Value.Schema)
	if Err != nil {
		return Result{}, Err
	}
	Generated.CurrentDatabaseID, Generated.ReferenceDatabaseID, Generated.Schema = DatabaseID, Value.ReferenceDatabaseID, Value.Schema
	return Generated, nil
}

func (Server *Service) Resolve(Ctx Context.Context, ID string) (string, error) {
	_, DSN, Err := Server.Connections.DialDSN(Ctx, ID)
	if Errors.Is(Err, Connection.ErrNotFound) {
		return "", ErrNotFound
	}
	if Err != nil {
		return "", ErrUnavailable
	}
	return DSN, nil
}
