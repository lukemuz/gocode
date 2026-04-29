package math_test

import (
	"context"
	"encoding/json"
	"testing"

	mathtools "github.com/lukemuz/gocode/tools/math"
)

func callCalc(t *testing.T, op string, a, b float64) string {
	t.Helper()
	calc := mathtools.New()
	input, _ := json.Marshal(map[string]any{"operation": op, "a": a, "b": b})
	got, err := calc.Func(context.Background(), input)
	if err != nil {
		t.Fatalf("calculator(%s, %g, %g): unexpected error: %v", op, a, b, err)
	}
	return got
}

func TestCalculatorAdd(t *testing.T) {
	if got := callCalc(t, "add", 3, 4); got != "7" {
		t.Errorf("3+4 = %q, want %q", got, "7")
	}
}

func TestCalculatorSubtract(t *testing.T) {
	if got := callCalc(t, "subtract", 10, 3); got != "7" {
		t.Errorf("10-3 = %q, want %q", got, "7")
	}
}

func TestCalculatorMultiply(t *testing.T) {
	if got := callCalc(t, "multiply", 6, 7); got != "42" {
		t.Errorf("6*7 = %q, want %q", got, "42")
	}
}

func TestCalculatorDivide(t *testing.T) {
	if got := callCalc(t, "divide", 10, 4); got != "2.5" {
		t.Errorf("10/4 = %q, want %q", got, "2.5")
	}
}

func TestCalculatorDivideByZero(t *testing.T) {
	calc := mathtools.New()
	input, _ := json.Marshal(map[string]any{"operation": "divide", "a": 1.0, "b": 0.0})
	_, err := calc.Func(context.Background(), input)
	if err == nil {
		t.Fatal("want error for division by zero, got nil")
	}
}

func TestCalculatorUnknownOperation(t *testing.T) {
	calc := mathtools.New()
	input, _ := json.Marshal(map[string]any{"operation": "modulo", "a": 10.0, "b": 3.0})
	_, err := calc.Func(context.Background(), input)
	if err == nil {
		t.Fatal("want error for unknown operation, got nil")
	}
}

func TestCalculatorToolName(t *testing.T) {
	calc := mathtools.New()
	if calc.Tool.Name != "calculator" {
		t.Errorf("want name %q, got %q", "calculator", calc.Tool.Name)
	}
}

func TestCalculatorToolset(t *testing.T) {
	calc := mathtools.New()
	ts := calc.Toolset()
	if len(ts.Bindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(ts.Bindings))
	}
	if _, ok := ts.Dispatch()["calculator"]; !ok {
		t.Error("dispatch missing calculator")
	}
}
