package Migration

import (
	Testing "testing"
)

func TestParseQueryPlanJSON(T *Testing.T) {
	SampleJSON := []byte(`[
		{
			"Plan": {
				"Node Type": "Hash Join",
				"Join Type": "Inner",
				"Startup Cost": 15.00,
				"Total Cost": 45.50,
				"Plan Rows": 100,
				"Actual Rows": 95,
				"Actual Total Time": 1.250,
				"Plans": [
					{
						"Node Type": "Seq Scan",
						"Relation Name": "orders",
						"Total Cost": 20.00,
						"Actual Rows": 100
					},
					{
						"Node Type": "Index Scan",
						"Relation Name": "customers",
						"Index Name": "customers_pkey",
						"Total Cost": 10.00,
						"Actual Rows": 95
					}
				]
			},
			"Planning Time": 0.350,
			"Execution Time": 1.450
		}
	]`)

	Analysis, Err := ParseQueryPlanJSON(SampleJSON)
	if Err != nil {
		T.Fatalf("unexpected error parsing plan JSON: %v", Err)
	}

	if Analysis.EstimatedCost != 45.50 {
		T.Errorf("expected estimated cost 45.50, got %f", Analysis.EstimatedCost)
	}
	if Analysis.ActualExecutionTime != 1.450 {
		T.Errorf("expected execution time 1.450, got %f", Analysis.ActualExecutionTime)
	}
	if Analysis.PlanningTime != 0.350 {
		T.Errorf("expected planning time 0.350, got %f", Analysis.PlanningTime)
	}
	if Analysis.RowsProcessed != 95 {
		T.Errorf("expected rows processed 95, got %f", Analysis.RowsProcessed)
	}

	// Verify Scan Types
	if len(Analysis.ScanTypes) != 2 || Analysis.ScanTypes[0] != "Seq Scan" || Analysis.ScanTypes[1] != "Index Scan" {
		T.Errorf("expected scan types [Seq Scan, Index Scan], got %v", Analysis.ScanTypes)
	}

	// Verify Indexes
	if len(Analysis.IndexesUsed) != 1 || Analysis.IndexesUsed[0] != "customers_pkey" {
		T.Errorf("expected index [customers_pkey], got %v", Analysis.IndexesUsed)
	}

	// Verify Joins
	if len(Analysis.JoinStrategies) != 1 || Analysis.JoinStrategies[0] != "Hash Join" {
		T.Errorf("expected join strategy [Hash Join], got %v", Analysis.JoinStrategies)
	}

	// Verify Nested Loop join recognition
	NestedLoopJSON := []byte(`[{"Plan": {"Node Type": "Nested Loop", "Join Type": "Inner"}}]`)
	NLAnalysis, Err := ParseQueryPlanJSON(NestedLoopJSON)
	if Err != nil {
		T.Fatalf("unexpected error parsing nested loop: %v", Err)
	}
	if len(NLAnalysis.JoinStrategies) != 1 || NLAnalysis.JoinStrategies[0] != "Nested Loop" {
		T.Errorf("expected [Nested Loop], got %v", NLAnalysis.JoinStrategies)
	}
}

func TestParseQueryPlanJSONInvalid(T *Testing.T) {
	_, Err := ParseQueryPlanJSON([]byte(`invalid json`))
	if Err == nil {
		T.Fatal("expected error on invalid json, got nil")
	}

	_, Err = ParseQueryPlanJSON([]byte(`[]`))
	if Err == nil {
		T.Fatal("expected error on empty array, got nil")
	}

	_, Err = ParseQueryPlanJSON([]byte(`[{"Other": 123}]`))
	if Err == nil {
		T.Fatal("expected error on missing Plan object, got nil")
	}
}
