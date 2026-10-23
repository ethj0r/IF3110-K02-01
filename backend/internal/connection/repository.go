package Connection

import (
	Context "context"
	Errors "errors"
	Pgx "github.com/jackc/pgx/v5"
	Pgconn "github.com/jackc/pgx/v5/pgconn"
	Pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *Pgxpool.Pool }

const ConnectionColumns = "C.id::text, C.name, C.host, C.port, C.dbname, C.username, C.created_at, C.updated_at"

const ConnectionReturning = "id::text, name, host, port, dbname, username, created_at, updated_at"

func ReadConnection(Row Pgx.Row) (Connection, error) {
	var Result Connection
	Err := Row.Scan(&Result.ID, &Result.Name, &Result.Host, &Result.Port, &Result.DBName, &Result.Username, &Result.CreatedAt, &Result.UpdatedAt)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if Err != nil {
		return Connection{}, ErrUnavailable
	}
	return Result, nil
}

func scanConnection(Row Pgx.Row) (Connection, error) {
	var Result Connection
	return Result, Row.Scan(&Result.ID, &Result.Name, &Result.Host, &Result.Port, &Result.DBName, &Result.Username, &Result.CreatedAt, &Result.UpdatedAt)
}

func isConflict(Err error) bool {
	var PgErr *Pgconn.PgError
	return Errors.As(Err, &PgErr) && PgErr.Code == "23505"
}

func conflictOrUnavailable(Err error, Missing error) error {
	if isConflict(Err) {
		return ErrConflict
	}
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return Missing
	}
	if Err != nil {
		return ErrUnavailable
	}
	return nil
}

func (Repo *Repository) Create(Ctx Context.Context, Input ConnectionInput, Ciphertext []byte, CreatedBy *string) (Connection, error) {
	Input = Input.Normalized()
	Row := Repo.Pool.QueryRow(Ctx, `INSERT INTO db_connections (name, host, port, dbname, username, password_ciphertext, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+ConnectionReturning, Input.Name, Input.Host, Input.Port, Input.DBName, Input.Username, Ciphertext, CreatedBy)
	Result, Err := scanConnection(Row)
	if Err := conflictOrUnavailable(Err, ErrUnavailable); Err != nil {
		return Connection{}, Err
	}
	return Result, nil
}

func (Repo *Repository) Get(Ctx Context.Context, ID string) (Connection, error) {
	return ReadConnection(Repo.Pool.QueryRow(Ctx, "SELECT "+ConnectionColumns+" FROM db_connections C WHERE C.id = $1", ID))
}

func (Repo *Repository) List(Ctx Context.Context) ([]Connection, error) {
	Rows, Err := Repo.Pool.Query(Ctx, "SELECT "+ConnectionColumns+" FROM db_connections C ORDER BY C.name")
	if Err != nil {
		return nil, ErrUnavailable
	}
	defer Rows.Close()
	Result := []Connection{}
	for Rows.Next() {
		Item, Err := ReadConnection(Rows)
		if Err != nil {
			return nil, Err
		}
		Result = append(Result, Item)
	}
	if Err := Rows.Err(); Err != nil {
		return nil, ErrUnavailable
	}
	return Result, nil
}

func (Repo *Repository) Update(Ctx Context.Context, ID string, Input ConnectionInput, Ciphertext []byte) (Connection, error) {
	Input = Input.Normalized()
	Row := Repo.Pool.QueryRow(Ctx, `UPDATE db_connections SET name = $2, host = $3, port = $4, dbname = $5, username = $6, password_ciphertext = $7, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 RETURNING `+ConnectionReturning, ID, Input.Name, Input.Host, Input.Port, Input.DBName, Input.Username, Ciphertext)
	Result, Err := scanConnection(Row)
	if Err := conflictOrUnavailable(Err, ErrNotFound); Err != nil {
		return Connection{}, Err
	}
	return Result, nil
}

func (Repo *Repository) Delete(Ctx Context.Context, ID string) error {
	Result, Err := Repo.Pool.Exec(Ctx, "DELETE FROM db_connections WHERE id = $1", ID)
	if Err != nil {
		return ErrUnavailable
	}
	if Result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (Repo *Repository) Ciphertext(Ctx Context.Context, ID string) ([]byte, error) {
	var Raw []byte
	Err := Repo.Pool.QueryRow(Ctx, "SELECT C.password_ciphertext FROM db_connections C WHERE C.id = $1", ID).Scan(&Raw)
	if Errors.Is(Err, Pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if Err != nil {
		return nil, ErrUnavailable
	}
	return Raw, nil
}
