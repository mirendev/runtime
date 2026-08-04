package workloadidentity

import (
	"fmt"
	"strings"
)

const subjectDelimiter = ":"

// Subject is the structured identity rendered into a workload token's sub
// claim. Its segments are private so every subject the issuer accepts has
// passed through the delimiter validation in newSubject.
type Subject struct {
	segments []subjectSegment
}

type subjectSegment struct {
	label string
	value string
}

func newSubject(segments ...subjectSegment) (Subject, error) {
	for _, segment := range segments {
		if segment.label == "" {
			return Subject{}, fmt.Errorf("subject segment label cannot be empty")
		}
		if strings.Contains(segment.label, subjectDelimiter) {
			return Subject{}, fmt.Errorf("subject segment label %q contains reserved delimiter %q", segment.label, subjectDelimiter)
		}
		if strings.Contains(segment.value, subjectDelimiter) {
			return Subject{}, fmt.Errorf("subject %s value %q contains reserved delimiter %q", segment.label, segment.value, subjectDelimiter)
		}
	}

	return Subject{segments: append([]subjectSegment(nil), segments...)}, nil
}

func newSandboxSubject(orgID, app, sandboxID string) (Subject, error) {
	var segments []subjectSegment
	if orgID != "" {
		segments = append(segments, subjectSegment{label: "org", value: orgID})
	}
	if app != "" {
		segments = append(segments, subjectSegment{label: "app", value: app})
	}
	segments = append(segments, subjectSegment{label: "sandbox", value: sandboxID})
	return newSubject(segments...)
}

func newSystemWorkloadSubject(orgID, clusterID string, workload SystemWorkload) (Subject, error) {
	var segments []subjectSegment
	if orgID != "" {
		segments = append(segments, subjectSegment{label: "org", value: orgID})
	}
	if clusterID != "" {
		segments = append(segments, subjectSegment{label: "cluster", value: clusterID})
	}
	segments = append(segments, subjectSegment{label: "system", value: string(workload)})
	return newSubject(segments...)
}

// String renders the subject as alternating colon-delimited labels and values.
func (s Subject) String() string {
	parts := make([]string, 0, len(s.segments)*2)
	for _, segment := range s.segments {
		parts = append(parts, segment.label, segment.value)
	}
	return strings.Join(parts, subjectDelimiter)
}

// parseSubject exists to pin the grammar's governing property in tests: every
// subject we emit must decode to exactly the segments that went in.
func parseSubject(encoded string) (Subject, error) {
	parts := strings.Split(encoded, subjectDelimiter)
	if len(parts)%2 != 0 {
		return Subject{}, fmt.Errorf("subject has an incomplete label/value pair")
	}

	segments := make([]subjectSegment, 0, len(parts)/2)
	for i := 0; i < len(parts); i += 2 {
		segments = append(segments, subjectSegment{label: parts[i], value: parts[i+1]})
	}
	return newSubject(segments...)
}
