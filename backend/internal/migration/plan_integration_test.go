//go:build integration

package Migration

import (
	Context "context"
	Errors "errors"
	Testing "testing"

	Access "github.com/ethj0r/IF3110-K02-01/backend/internal/access"
	Auth "github.com/ethj0r/IF3110-K02-01/backend/internal/auth"
	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

func TestServiceAnalyzePlanRealPostgres(T *Testing.T) {
	PlatformPool := NewMigrationDatabase(T)
	TargetPool := NewMigrationDatabase(T)
	Ctx := Context.Background()

	DBID, UserID := setupFixture(T, PlatformPool)
	Editor := Auth.Principal{ID: UserID, Username: "plan_tester"}
	ViewerWithoutAccess := Auth.Principal{ID: "no-access-user", Username: "stranger"}

	// Create a real table with primary key in target database
	_, Err := TargetPool.Exec(Ctx, `
		CREATE TABLE plan_items (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			price NUMERIC(10, 2)
		);
		INSERT INTO plan_items (name, price) VALUES ('Widget', 9.99), ('Gadget', 19.99);
	`)
	if Err != nil {
		T.Fatalf("failed to setup target table: %v", Err)
	}

	Svc := &Service{
		Repo:   &Repository{Pool: PlatformPool},
		Access: Access.AllowAll(),
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://target-db", nil
		}),
		Driver: testTargetDriver{Pool: TargetPool},
	}

	// 1. Analyze SELECT with index
	SelectPlan, Err := Svc.AnalyzePlan(Ctx, Editor, DBID, "SELECT * FROM plan_items WHERE id = 1")
	if Err != nil {
		T.Fatalf("failed to analyze SELECT query: %v", Err)
	}

	if SelectPlan.EstimatedCost <= 0 {
		T.Errorf("expected positive estimated cost, got %f", SelectPlan.EstimatedCost)
	}
	if SelectPlan.ActualExecutionTime < 0 {
		T.Errorf("expected non-negative execution time, got %f", SelectPlan.ActualExecutionTime)
	}
	if len(SelectPlan.ScanTypes) == 0 {
		T.Errorf("expected at least one scan type, got %v", SelectPlan.ScanTypes)
	}

	// 2. Analyze INSERT query and verify rollback (target data must not be persisted)
	InsertPlan, Err := Svc.AnalyzePlan(Ctx, Editor, DBID, "INSERT INTO plan_items (name, price) VALUES ('Ephemeral', 99.99)")
	if Err != nil {
		T.Fatalf("failed to analyze INSERT query: %v", Err)
	}
	if InsertPlan.EstimatedCost < 0 {
		T.Errorf("expected valid cost, got %f", InsertPlan.EstimatedCost)
	}

	// Verify target table still has only 2 rows (INSERT was rolled back!)
	var Count int
	if Err := TargetPool.QueryRow(Ctx, "SELECT COUNT(*) FROM plan_items WHERE name = 'Ephemeral'").Scan(&Count); Err != nil {
		T.Fatalf("failed to count rows: %v", Err)
	}
	if Count != 0 {
		T.Fatalf("EXPLAIN ANALYZE mutated target database, expected 0 rows, got %d", Count)
	}

	// 3. Analyze invalid query
	_, Err = Svc.AnalyzePlan(Ctx, Editor, DBID, "SELECT * FROM non_existent_table")
	if !Errors.Is(Err, Driver.ErrExecution) {
		T.Fatalf("expected ErrExecution for invalid table, got %v", Err)
	}

	// 4. Analyze empty SQL
	_, Err = Svc.AnalyzePlan(Ctx, Editor, DBID, "   ")
	if !Errors.Is(Err, ErrInvalidInput) {
		T.Fatalf("expected ErrInvalidInput for empty SQL, got %v", Err)
	}

	// 5. Test access control
	RestrictedSvc := &Service{
		Repo:   &Repository{Pool: PlatformPool},
		Access: Access.StaticEvaluator{Grants: map[string]map[string]Access.Level{}}, // Deny all
		Resolver: DSNResolverFunc(func(_ Context.Context, _ string) (string, error) {
			return "postgres://target-db", nil
		}),
		Driver: testTargetDriver{Pool: TargetPool},
	}
	_, Err = RestrictedSvc.AnalyzePlan(Ctx, ViewerWithoutAccess, DBID, "SELECT 1")
	if !Errors.Is(Err, ErrForbidden) {
		T.Fatalf("expected ErrForbidden for unauthorized user, got %v", Err)
	}
}
