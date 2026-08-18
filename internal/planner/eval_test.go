package planner

import "testing"

func TestPlannerV1EvalSuite(t *testing.T) {
	report, err := RunEvalSuite("planner/v1")
	if err != nil {
		t.Fatalf("RunEvalSuite() error = %v", err)
	}
	if report.Total < 5 || report.Passed != report.Total || len(report.Failures) != 0 {
		t.Fatalf("report = %+v", report)
	}
}
