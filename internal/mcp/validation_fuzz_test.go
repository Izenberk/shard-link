package mcp

import (
	"testing"
)

// validateSaveInput checks MCP save_memory input constraints.
// Extracted from handleSave for testability — same rules, no server dependency.
//
// Rules:
//   - ID: max 256 chars, must not be empty
//   - Content: max 100KB, must not be empty
//   - Category: max 50 chars, must be in allowedCategories whitelist
//
// Returns an error string if invalid, empty string if valid.
func validateSaveInput(id, content, category string, allowCore bool) string {
	if id == "" {
		return "ID must not be empty"
	}
	if len(id) > maxIDLen {
		return "ID exceeds max length"
	}
	if content == "" {
		return "Content must not be empty"
	}
	if len(content) > maxContentLen {
		return "Content exceeds max size"
	}
	if len(category) > maxCategoryLen {
		return "Category exceeds max length"
	}
	if !allowedCategories[category] {
		return "Category not allowed"
	}
	if category == "core" && !allowCore {
		return "category 'core' requires allow_core=true"
	}
	return ""
}

// validateQueryInput checks MCP search input constraints.
// Extracted from handleSearchAll for testability.
//
// Rules:
//   - Query: max 10,000 chars, must not be empty
//   - Limit: clamped to [1, 100]
//
// Returns the clamped limit and an error string (empty if valid).
func validateQueryInput(query string, limit int) (int, string) {
	if query == "" {
		return limit, "Query must not be empty"
	}
	if len(query) > maxQueryLen {
		return limit, "Query exceeds max length"
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxResultLimit {
		limit = maxResultLimit
	}
	return limit, ""
}

// --- Table-driven unit tests ---

func TestValidateSaveInput(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		content   string
		category  string
		allowCore bool
		wantErr   bool
	}{
		{
			name:     "valid input",
			id:       "test-shard-1",
			content:  "some content",
			category: "memory",
			wantErr:  false,
		},
		{
			name:     "empty ID rejected",
			id:       "",
			content:  "some content",
			category: "memory",
			wantErr:  true,
		},
		{
			name:     "empty content rejected",
			id:       "test-shard",
			content:  "",
			category: "memory",
			wantErr:  true,
		},
		{
			name:      "core blocked without allow_core flag",
			id:        "test-shard",
			content:   "content",
			category:  "core",
			allowCore: false,
			wantErr:   true,
		},
		{
			name:      "core allowed with allow_core flag",
			id:        "test-shard",
			content:   "content",
			category:  "core",
			allowCore: true,
			wantErr:   false,
		},
		{
			name:     "unknown category rejected",
			id:       "test-shard",
			content:  "content",
			category: "evil",
			wantErr:  true,
		},
		{
			name:     "oversized ID rejected",
			id:       string(make([]byte, maxIDLen+1)),
			content:  "content",
			category: "memory",
			wantErr:  true,
		},
		{
			name:     "max size content accepted",
			id:       "test-shard",
			content:  string(make([]byte, maxContentLen)),
			category: "memory",
			wantErr:  false,
		},
		{
			name:     "oversized content rejected",
			id:       "test-shard",
			content:  string(make([]byte, maxContentLen+1)),
			category: "memory",
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errMsg := validateSaveInput(tc.id, tc.content, tc.category, tc.allowCore)
			gotErr := errMsg != ""
			if gotErr != tc.wantErr {
				t.Errorf("validateSaveInput(%q, len=%d, %q) err=%q, wantErr=%v",
					tc.id, len(tc.content), tc.category, errMsg, tc.wantErr)
			}
		})
	}
}

func TestValidateQueryInput(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		limit     int
		wantLimit int
		wantErr   bool
	}{
		{
			name:      "valid query",
			query:     "search term",
			limit:     5,
			wantLimit: 5,
			wantErr:   false,
		},
		{
			name:      "empty query rejected",
			query:     "",
			limit:     5,
			wantLimit: 5,
			wantErr:   true,
		},
		{
			name:      "limit clamped to max",
			query:     "test",
			limit:     500,
			wantLimit: 100,
			wantErr:   false,
		},
		{
			name:      "limit floored at 1",
			query:     "test",
			limit:     -5,
			wantLimit: 1,
			wantErr:   false,
		},
		{
			name:      "oversized query rejected",
			query:     string(make([]byte, maxQueryLen+1)),
			limit:     5,
			wantLimit: 5,
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotLimit, errMsg := validateQueryInput(tc.query, tc.limit)
			gotErr := errMsg != ""
			if gotErr != tc.wantErr {
				t.Errorf("validateQueryInput(len=%d, %d) err=%q, wantErr=%v",
					len(tc.query), tc.limit, errMsg, tc.wantErr)
			}
			if !gotErr && gotLimit != tc.wantLimit {
				t.Errorf("validateQueryInput limit=%d, want=%d", gotLimit, tc.wantLimit)
			}
		})
	}
}

// --- Fuzz tests ---

// FuzzValidateSaveInput throws random inputs at the save validator.
// It must never panic and must always return a string (empty or error message).
//
// Run: go test -fuzz=FuzzValidateSaveInput -fuzztime=60s ./internal/mcp/
func FuzzValidateSaveInput(f *testing.F) {
	// Seed the corpus with representative inputs
	f.Add("valid-id", "valid content", "memory")
	f.Add("", "content", "memory")
	f.Add("id", "", "session")
	f.Add("id", "content", "evil")
	f.Add("id", "content", "")
	f.Add(string(make([]byte, 300)), "content", "memory")

	f.Fuzz(func(t *testing.T, id, content, category string) {
		// Must never panic — this is the primary fuzz contract.
		result := validateSaveInput(id, content, category, false)

		// core is excluded: with allowCore=false, core is always an error even though
		// it's in allowedCategories. The fuzz test doesn't parametrize allowCore.
		if id != "" && len(id) <= maxIDLen &&
			content != "" && len(content) <= maxContentLen &&
			len(category) <= maxCategoryLen && allowedCategories[category] &&
			category != "core" {
			if result != "" {
				t.Errorf("expected valid input to pass, got error: %s", result)
			}
		}

		_ = result // ensure no unused variable
	})
}

// FuzzValidateQueryInput throws random inputs at the query validator.
//
// Run: go test -fuzz=FuzzValidateQueryInput -fuzztime=60s ./internal/mcp/
func FuzzValidateQueryInput(f *testing.F) {
	f.Add("search term", 5)
	f.Add("", 0)
	f.Add("x", -1)
	f.Add("x", 999)
	f.Add(string(make([]byte, 20000)), 5)

	f.Fuzz(func(t *testing.T, query string, limit int) {
		// Must never panic.
		gotLimit, errMsg := validateQueryInput(query, limit)

		// Limit must always be clamped to [1, 100] when valid.
		if errMsg == "" {
			if gotLimit < 1 || gotLimit > maxResultLimit {
				t.Errorf("limit %d out of bounds [1, %d]", gotLimit, maxResultLimit)
			}
		}
	})
}
