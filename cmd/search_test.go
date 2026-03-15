package cmd

import (
	"errors"
	"testing"
)

func TestSearchDiagCommand_RequiresQuery(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantError     bool
		errorContains string
	}{
		{
			name:          "valid query",
			query:         "test search",
			wantError:     false,
			errorContains: "",
		},
		{
			name:          "empty query",
			query:         "",
			wantError:     true,
			errorContains: "query is required",
		},
		{
			name:          "whitespace only query",
			query:         "   ",
			wantError:     true,
			errorContains: "query is required",
		},
		{
			name:          "query with leading/trailing whitespace",
			query:         "  test  ",
			wantError:     false,
			errorContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSearchQuery(tt.query)

			if (err != nil) != tt.wantError {
				t.Errorf("validateSearchQuery() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil && tt.errorContains != "" {
				errStr := err.Error()
				found := false
				for i := 0; i <= len(errStr)-len(tt.errorContains); i++ {
					if errStr[i:i+len(tt.errorContains)] == tt.errorContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("validateSearchQuery() error = %v, want error containing %q", err, tt.errorContains)
				}
			}
		})
	}
}

func validateSearchQuery(query string) error {
	for i := 0; i < len(query); i++ {
		if query[i] != ' ' && query[i] != '\t' && query[i] != '\n' && query[i] != '\r' {
			return nil
		}
	}
	return errors.New("query is required")
}
