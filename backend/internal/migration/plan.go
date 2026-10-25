package Migration

import (
	Json "encoding/json"
	Strings "strings"

	Driver "github.com/ethj0r/IF3110-K02-01/backend/internal/driver"
)

type QueryPlanAnalysis struct {
	ScanTypes           []string `json:"scan_types"`
	IndexesUsed         []string `json:"indexes_used"`
	JoinStrategies      []string `json:"join_strategies"`
	EstimatedCost       float64  `json:"estimated_cost"`
	ActualExecutionTime float64  `json:"actual_execution_time"`
	RowsProcessed       float64  `json:"rows_processed"`
	PlanningTime        float64  `json:"planning_time"`
	RawPlan             any      `json:"raw_plan,omitempty"`
}

func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func parsePlanNode(node map[string]any, analysis *QueryPlanAnalysis) {
	if nodeType, ok := node["Node Type"].(string); ok && nodeType != "" {
		lower := Strings.ToLower(nodeType)
		if Strings.Contains(lower, "scan") && !containsString(analysis.ScanTypes, nodeType) {
			analysis.ScanTypes = append(analysis.ScanTypes, nodeType)
		}
		if (Strings.Contains(lower, "join") || Strings.Contains(lower, "nested loop") || node["Join Type"] != nil) && !containsString(analysis.JoinStrategies, nodeType) {
			analysis.JoinStrategies = append(analysis.JoinStrategies, nodeType)
		}
	}
	if idxName, ok := node["Index Name"].(string); ok && idxName != "" {
		if !containsString(analysis.IndexesUsed, idxName) {
			analysis.IndexesUsed = append(analysis.IndexesUsed, idxName)
		}
	}
	if subPlans, ok := node["Plans"].([]any); ok {
		for _, sub := range subPlans {
			if subMap, ok := sub.(map[string]any); ok {
				parsePlanNode(subMap, analysis)
			}
		}
	}
}

func ParseQueryPlanJSON(data []byte) (QueryPlanAnalysis, error) {
	var list []map[string]any
	if err := Json.Unmarshal(data, &list); err != nil || len(list) == 0 {
		return QueryPlanAnalysis{}, Driver.ErrExecution
	}
	root := list[0]
	planMap, _ := root["Plan"].(map[string]any)
	if planMap == nil {
		return QueryPlanAnalysis{}, Driver.ErrExecution
	}

	analysis := QueryPlanAnalysis{
		ScanTypes:      []string{},
		IndexesUsed:    []string{},
		JoinStrategies: []string{},
		RawPlan:        list,
	}

	if cost, ok := planMap["Total Cost"].(float64); ok {
		analysis.EstimatedCost = cost
	}
	if rows, ok := planMap["Actual Rows"].(float64); ok {
		analysis.RowsProcessed = rows
	} else if rows, ok := planMap["Plan Rows"].(float64); ok {
		analysis.RowsProcessed = rows
	}

	if execTime, ok := root["Execution Time"].(float64); ok {
		analysis.ActualExecutionTime = execTime
	} else if execTime, ok := planMap["Actual Total Time"].(float64); ok {
		analysis.ActualExecutionTime = execTime
	}

	if planTime, ok := root["Planning Time"].(float64); ok {
		analysis.PlanningTime = planTime
	}

	parsePlanNode(planMap, &analysis)

	return analysis, nil
}
