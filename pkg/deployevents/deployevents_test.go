package deployevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"miren.dev/runtime/pkg/apphealth"
	"miren.dev/runtime/pkg/deploylifecycle"
)

func TestWriterRoundTripsThroughRecord(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	w.Emit(Start{Header: NewHeader(EventStart), App: "meet", Cluster: "prod"})
	w.Emit(Deployment{Header: NewHeader(EventDeployment), DeployID: "dpl_1", Phase: deploylifecycle.PhaseActivating})
	w.Emit(Health{Header: NewHeader(EventHealth), Version: "meet-v1", Outcome: OutcomeScaledToZero, OK: true, Health: apphealth.Idle})
	r := Result{App: "meet", DeployID: "dpl_1", AppVersion: "meet-v1", URLs: []string{}, Status: deploylifecycle.StatusSucceeded}
	r.SetEphemeral("pr-1", "24h")
	w.Emit(ResultEvent{Header: NewHeader(EventResult), Result: r})

	if w.Err() != nil {
		t.Fatalf("unexpected write error: %v", w.Err())
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), buf.String())
	}
	var recs []Record
	for i, line := range lines {
		if !strings.HasPrefix(line, `{"event":"`) {
			t.Errorf("line %d must start with the event key: %s", i+1, line)
		}
		var rec Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		recs = append(recs, rec)
	}

	if recs[0].Event != EventStart || recs[0].App != "meet" || recs[0].Cluster != "prod" {
		t.Errorf("start = %+v", recs[0])
	}
	if recs[1].Event != EventDeployment || recs[1].Phase != string(deploylifecycle.PhaseActivating) {
		t.Errorf("deployment = %+v", recs[1])
	}
	if recs[2].Outcome != OutcomeScaledToZero || !recs[2].OK || recs[2].Health != apphealth.Idle {
		t.Errorf("health = %+v", recs[2])
	}
	if recs[3].Event != EventResult || recs[3].Status != string(deploylifecycle.StatusSucceeded) ||
		recs[3].Ephemeral == nil || recs[3].Ephemeral.Label != "pr-1" {
		t.Errorf("result = %+v", recs[3])
	}
}

// TestResultStatusIsTheDeploymentRecordVocabulary pins the contract the
// reviewer asked for: the spelling a script branches on here must be the one
// `miren app history` reports for the same deployment.
func TestResultStatusIsTheDeploymentRecordVocabulary(t *testing.T) {
	for _, s := range []deploylifecycle.Status{deploylifecycle.StatusSucceeded, deploylifecycle.StatusFailed, deploylifecycle.StatusCancelled} {
		data, err := json.Marshal(Result{Status: s, URLs: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"status":"`+string(s)+`"`) {
			t.Errorf("status %q not serialized as itself: %s", s, data)
		}
		if !s.Valid() {
			t.Errorf("status %q is not a valid deploylifecycle status", s)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriterRemembersFirstError(t *testing.T) {
	w := NewWriter(failingWriter{})
	if w.Err() != nil {
		t.Fatal("no error before any write")
	}
	w.Emit(Start{Header: NewHeader(EventStart)})
	w.Emit(Message{Header: NewHeader(EventMessage), Message: "still going"})
	if w.Err() == nil || w.Err().Error() != "broken pipe" {
		t.Fatalf("Err() = %v, want the first write failure", w.Err())
	}
}

func TestSetEphemeral(t *testing.T) {
	var r Result
	r.SetEphemeral("pr-1", "24h")
	if r.Ephemeral == nil || r.Ephemeral.Label != "pr-1" || r.Ephemeral.TTL != "24h" {
		t.Fatalf("ephemeral = %+v", r.Ephemeral)
	}
	r.SetEphemeral("", "")
	if r.Ephemeral != nil {
		t.Fatal("empty label must clear the block")
	}
}
