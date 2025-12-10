package logfilter

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		wantErr bool
	}{
		{"empty", "", true, false},
		{"whitespace only", "   ", true, false},
		{"single word", "error", false, false},
		{"multiple words", "error timeout", false, false},
		{"negated word", "-debug", false, false},
		{"word with negation", "error -debug", false, false},
		{"simple regex", "/err.*/", false, false},
		{"negated regex", "-/debug/", false, false},
		{"mixed", "error /warn.*/ -debug", false, false},
		{"unclosed regex treated as word", "/unclosed", false, false},
		{"invalid regex", "/[invalid/", true, true},
		// Quoted strings
		{"double quoted phrase", `"error message"`, false, false},
		{"single quoted phrase", `'error message'`, false, false},
		{"negated quoted phrase", `-"debug info"`, false, false},
		{"mixed with quotes", `error "connection failed" -debug`, false, false},
		{"empty quotes", `""`, true, false},
		{"unclosed double quote", `"unclosed`, false, false},
		{"unclosed single quote", `'unclosed`, false, false},
		{"escaped quote in phrase", `"say \"hello\""`, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if (f == nil) != tt.wantNil {
				t.Errorf("Parse(%q) = %v, wantNil %v", tt.input, f, tt.wantNil)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		line   string
		want   bool
	}{
		// Word matching (case-insensitive)
		{"word matches", "error", "This is an error message", true},
		{"word matches case insensitive", "ERROR", "this is an error message", true},
		{"word no match", "error", "This is a warning message", false},
		{"partial word match", "err", "This is an error message", true},

		// Multiple words (AND logic)
		{"multiple words all match", "error timeout", "Connection error due to timeout", true},
		{"multiple words partial match", "error timeout", "Connection error occurred", false},
		{"multiple words order independent", "timeout error", "error happened then timeout", true},

		// Negation
		{"negated word excludes", "-debug", "Debug: some message", false},
		{"negated word includes", "-debug", "Error: some message", true},
		{"word and negation", "error -debug", "Error: something failed", true},
		{"word and negation excludes", "error -debug", "Debug: Error occurred", false},

		// Regex
		{"regex matches", "/err(or)?/", "This is an error", true},
		{"regex matches partial", "/err(or)?/", "This is err", true},
		{"regex no match", "/^error$/", "This is an error", false},
		{"regex case sensitive", "/Error/", "this is an error", false},
		{"regex case insensitive flag", "/(?i)Error/", "this is an error", true},

		// Negated regex (regex is case-sensitive)
		{"negated regex excludes", "-/debug/", "debug message", false},
		{"negated regex includes", "-/debug/", "Error message", true},
		{"negated regex case mismatch", "-/debug/", "Debug message", true}, // regex doesn't match, so negation passes

		// Mixed
		{"mixed word and regex", "error /warn(ing)?/", "error and warning here", true},
		{"mixed word and regex partial", "error /warn(ing)?/", "error only", false},
		{"mixed with negation", "error -/debug/", "error occurred", true},
		{"mixed with negation excludes", "error -/debug/", "debug error", false},

		// Nil filter matches everything
		{"nil filter", "", "anything", true},

		// Quoted phrases
		{"quoted phrase matches", `"connection failed"`, "Error: connection failed at startup", true},
		{"quoted phrase case insensitive", `"Connection Failed"`, "error: connection failed", true},
		{"quoted phrase no match", `"connection failed"`, "connection was successful", false},
		{"quoted phrase with spaces", `"hello world"`, "message: hello world!", true},
		{"quoted phrase partial no match", `"hello world"`, "hello there world", false},
		{"single quoted phrase", `'error occurred'`, "Error occurred in module", true},
		{"negated quoted phrase excludes", `-"debug info"`, "debug info: checking", false},
		{"negated quoted phrase includes", `-"debug info"`, "error occurred", true},
		{"mixed word and phrase", `error "connection timeout"`, "error: connection timeout happened", true},
		{"mixed word and phrase partial", `error "connection timeout"`, "error: connection refused", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.filter)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			if got := f.Match(tt.line); got != tt.want {
				t.Errorf("Filter(%q).Match(%q) = %v, want %v", tt.filter, tt.line, got, tt.want)
			}
		})
	}
}

func TestToLogsQL(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		want   string
	}{
		{"empty", "", ""},
		{"single word", "error", "error"},
		{"multiple words", "error timeout", "error timeout"},
		{"negated word", "-debug", "-debug"},
		{"word with negation", "error -debug", "error -debug"},
		{"simple regex", "/err.*/", `~"err.*"`},
		{"negated regex", "-/debug/", `-~"debug"`},
		{"mixed", "error /warn.*/ -debug", `error ~"warn.*" -debug`},
		{"regex with quotes", `/say "hello"/`, `~"say \"hello\""`},
		{"regex with backslash", `/path\\file/`, `~"path\\\\file"`},
		// Quoted phrases
		{"quoted phrase", `"connection failed"`, `"connection failed"`},
		{"negated phrase", `-"debug info"`, `-"debug info"`},
		{"phrase with special chars", `"error: failed"`, `"error: failed"`},
		{"mixed with phrase", `error "timeout occurred"`, `error "timeout occurred"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.filter)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			if got := f.ToLogsQL(); got != tt.want {
				t.Errorf("Filter(%q).ToLogsQL() = %q, want %q", tt.filter, got, tt.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		want   string
	}{
		{"empty", "", ""},
		{"single word", "error", "error"},
		{"multiple words", "error timeout", "error timeout"},
		{"negated word", "-debug", "-debug"},
		{"regex", "/pattern/", "/pattern/"},
		{"negated regex", "-/pattern/", "-/pattern/"},
		{"mixed", "error /warn/ -debug", "error /warn/ -debug"},
		// Quoted phrases (always output with double quotes)
		{"double quoted phrase", `"error message"`, `"error message"`},
		{"single quoted phrase", `'error message'`, `"error message"`},
		{"negated phrase", `-"debug info"`, `-"debug info"`},
		{"phrase with escaped quote", `"say \"hi\""`, `"say \"hi\""`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.filter)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			if got := f.String(); got != tt.want {
				t.Errorf("Filter(%q).String() = %q, want %q", tt.filter, got, tt.want)
			}
		})
	}
}

func TestParseEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		terms  int
		wantOK bool
	}{
		{"trailing dash", "error -", 1, true},
		{"only dash", "-", 0, true},
		{"escaped in regex", `/test\//`, 1, true},
		{"tabs as whitespace", "error\ttimeout", 2, true},
		{"multiple spaces", "error    timeout", 2, true},
		{"trailing whitespace", "error   ", 1, true},
		{"leading and trailing whitespace", "  error  ", 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.input)
			if err != nil {
				if tt.wantOK {
					t.Errorf("Parse(%q) unexpected error = %v", tt.input, err)
				}
				return
			}
			if !tt.wantOK {
				t.Errorf("Parse(%q) expected error", tt.input)
				return
			}
			if f == nil && tt.terms > 0 {
				t.Errorf("Parse(%q) = nil, want %d terms", tt.input, tt.terms)
				return
			}
			if f != nil && len(f.terms) != tt.terms {
				t.Errorf("Parse(%q) has %d terms, want %d", tt.input, len(f.terms), tt.terms)
			}
		})
	}
}

func TestUTF8Support(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		line   string
		want   bool
	}{
		// UTF-8 word matching
		{"japanese word", "エラー", "システムエラーが発生しました", true},
		{"chinese word", "错误", "发生了一个错误", true},
		{"emoji filter", "🔥", "Server is on 🔥", true},
		{"cyrillic word", "ошибка", "Произошла ошибка", true},
		{"mixed utf8 and ascii", "error エラー", "error エラー occurred", true},

		// UTF-8 in quoted phrases
		{"quoted japanese phrase", `"接続エラー"`, "Error: 接続エラーが発生", true},
		{"quoted emoji phrase", `"🎉 success"`, "Result: 🎉 success!", true},

		// UTF-8 in regex
		{"regex with unicode", `/エラー.+/`, "エラーが発生しました", true},

		// Negation with UTF-8
		{"negated utf8", "-デバッグ", "エラーが発生しました", true},
		{"negated utf8 excludes", "-デバッグ", "デバッグ: チェック中", false},

		// Case insensitivity for latin extended
		{"german umlaut case", "MÜLLER", "Herr müller ist hier", true},
		{"french accent case", "café", "Welcome to CAFÉ Paris", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.filter)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			if got := f.Match(tt.line); got != tt.want {
				t.Errorf("Filter(%q).Match(%q) = %v, want %v", tt.filter, tt.line, got, tt.want)
			}
		})
	}
}

func TestMatchRealWorldLogs(t *testing.T) {
	tests := []struct {
		name   string
		filter string
		line   string
		want   bool
	}{
		// Typical log formats
		{"json log error field", "error", `{"level":"error","msg":"connection failed","time":"2024-01-01T00:00:00Z"}`, true},
		{"json log info field", "error", `{"level":"info","msg":"server started","time":"2024-01-01T00:00:00Z"}`, false},
		{"syslog format", "error", "Jan  1 00:00:00 host app[1234]: ERROR: connection failed", true},
		{"klog format", "error", "E0101 00:00:00.000000       1 main.go:42] error occurred", true},

		// HTTP status codes
		{"http 500 error", "500", "192.168.1.1 - - [01/Jan/2024:00:00:00] \"GET /api HTTP/1.1\" 500 1234", true},
		{"http 200 ok", "500", "192.168.1.1 - - [01/Jan/2024:00:00:00] \"GET /api HTTP/1.1\" 200 1234", false},

		// Stack traces
		{"panic line", "panic", "panic: runtime error: invalid memory address", true},
		{"goroutine", "/goroutine/", "goroutine 1 [running]:", true},

		// Empty and whitespace lines
		{"empty line no filter", "", "", true},
		{"empty line with filter", "error", "", false},
		{"whitespace line", "error", "   ", false},

		// Special characters in logs
		{"brackets in log", "error", "[ERROR] something went wrong", true},
		{"parens in log", "failed", "Connection failed (timeout)", true},
		{"equals in log", "status=500", "request completed status=500 duration=1.5s", true},

		// UUID/ID matching
		{"uuid partial", "abc123", "Request ID: abc123-def456-ghi789", true},
		{"trace id", "/[a-f0-9]{8}/", "trace_id=deadbeef span_id=12345678", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Parse(tt.filter)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			if got := f.Match(tt.line); got != tt.want {
				t.Errorf("Filter(%q).Match(%q) = %v, want %v", tt.filter, tt.line, got, tt.want)
			}
		})
	}
}

func TestMatchOnlyNegation(t *testing.T) {
	// Filter with only negation should match lines that don't contain the term
	f, err := Parse("-debug -trace")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	tests := []struct {
		line string
		want bool
	}{
		{"error occurred", true},
		{"debug message", false},
		{"trace started", false},
		{"debug and trace", false},
		{"info log", true},
	}

	for _, tt := range tests {
		if got := f.Match(tt.line); got != tt.want {
			t.Errorf("Filter(-debug -trace).Match(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestFilterReuse(t *testing.T) {
	// Verify filter can be reused multiple times
	f, err := Parse("error -debug")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	lines := []string{
		"error occurred",
		"debug message",
		"error in debug mode",
		"warning message",
		"another error",
	}
	expected := []bool{true, false, false, false, true}

	for i, line := range lines {
		if got := f.Match(line); got != expected[i] {
			t.Errorf("Filter.Match(%q) = %v, want %v", line, got, expected[i])
		}
	}

	// Run again to ensure filter state isn't modified
	for i, line := range lines {
		if got := f.Match(line); got != expected[i] {
			t.Errorf("Second run: Filter.Match(%q) = %v, want %v", line, got, expected[i])
		}
	}
}

func BenchmarkParse(b *testing.B) {
	inputs := []string{
		"error",
		"error timeout -debug",
		"/err(or)?/ warning -/debug/",
	}

	for _, input := range inputs {
		b.Run(input, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_, _ = Parse(input)
			}
		})
	}
}

func BenchmarkMatch(b *testing.B) {
	filters := []string{
		"error",
		"error timeout",
		"error -debug",
		"/err(or)?/",
	}
	line := "2024-01-01T00:00:00Z ERROR: connection timeout occurred in debug mode"

	for _, filterStr := range filters {
		f, _ := Parse(filterStr)
		b.Run(filterStr, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				f.Match(line)
			}
		})
	}
}
