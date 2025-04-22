package shellwords_test

import (
	"reflect"
	"testing"

	"github.com/buildkite/shellwords"
)

func TestSplitBatch(t *testing.T) {
	var testCases = []struct {
		String   string
		Expected []string
	}{
		{
			`"\\vmware-host\Shared Folders\src\github.com\buildkite\agent\llamas @% test\buildkite-agent.exe" start`, []string{
				`\\vmware-host\Shared Folders\src\github.com\buildkite\agent\llamas @% test\buildkite-agent.exe`,
				`start`,
			},
		},
		{
			`simple 🙌🏻 --string "quo""ted"`, []string{
				`simple`,
				`🙌🏻`,
				`--string`,
				`quo"ted`,
			},
		},

		{
			`simple --string "quo""ted"`, []string{
				`simple`,
				`--string`,
				`quo"ted`,
			},
		},
		{
			`mkdir "My favorite "^%OS^%`, []string{
				`mkdir`,
				`My favorite %OS%`,
			},
		},
		{
			`runme.exe /password:"~!@#$^%^^^&*()_+^|-=\][{}'^;:""/.>?,<"`, []string{
				`runme.exe`,
				`/password:~!@#$%^&*()_+|-=\][{}';:"/.>?,<`,
			},
		},
		{
			`echo ^^^^^&`, []string{
				`echo`,
				`^^&`,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run("", func(t *testing.T) {
			actual, err := shellwords.SplitBatch(tc.String)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(tc.Expected, actual) {
				t.Fatalf("Expected vs Actual: \n%#v\n\n%#v", tc.Expected, actual)
			}
		})
	}
}

func TestQuoteBatch(t *testing.T) {
	var testCases = []struct {
		String   string
		Expected string
	}{
		{`nothing_needed`, `nothing_needed`},
		{`C:\bin\bash`, `C:\bin\bash`},
		{`C:\Program Files\bin\bash.exe`, `"C:\Program Files\bin\bash.exe"`},
		{`\\uncpath\My Files\bin\bash.exe`, `"\\uncpath\My Files\bin\bash.exe"`},
		{`this has spaces`, `"this has spaces"`},
		{`this has $pace$`, `"this has $pace$"`},
		{`this has %spaces%`, `"this has ^%spaces^%"`},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run("", func(t *testing.T) {
			actual := shellwords.QuoteBatch(tc.String)

			if tc.Expected != actual {
				t.Fatalf("Expected vs Actual: \n%#v\n\n%#v", tc.Expected, actual)
			}
		})
	}
}
