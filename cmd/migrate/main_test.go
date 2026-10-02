package main

import (
	"io"
	"strings"
	"testing"
)

// Argument errors are reported before any database connection is attempted.
func TestRunRejectsInvalidArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no command", nil, "expected exactly one command"},
		{"too many commands", []string{"up", "down"}, "expected exactly one command"},
		{"unknown command", []string{"force"}, `unknown command "force"`},
		{"unknown flag", []string{"-nope", "up"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLICKHOUSE_DSN", "clickhouse://invalid:1/none")
			err := run(tt.args, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("run(%q) error = %v, want containing %q", tt.args, err, tt.want)
			}
		})
	}
}
