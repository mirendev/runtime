package lsvd

import (
	"math/rand"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

// Shared entropy source for consistent ULIDs in tests
var testEntropy = rand.New(rand.NewSource(1))

// Helper to create SegmentIds with specific timestamps
func makeSegmentIds(timestamps ...uint64) []SegmentId {
	result := make([]SegmentId, len(timestamps))
	for i, ts := range timestamps {
		result[i] = SegmentId(ulid.MustNew(ts, testEntropy))
	}
	return result
}

func TestComposeSegmentList(t *testing.T) {
	r := require.New(t)

	// Create 7 test segment IDs with incrementing timestamps
	baseTime := uint64(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	allSegs := makeSegmentIds(
		baseTime+1000,
		baseTime+2000,
		baseTime+3000,
		baseTime+4000,
		baseTime+5000,
		baseTime+6000,
		baseTime+7000,
	)

	t.Run("empty lists", func(t *testing.T) {
		result, err := composeSegmentList(nil, nil)
		r.NoError(err)
		r.Empty(result)
	})

	t.Run("primary empty", func(t *testing.T) {
		replica := allSegs[0:3]
		result, err := composeSegmentList(nil, replica)
		r.NoError(err)
		r.Equal(replica, result)
	})

	t.Run("replica empty", func(t *testing.T) {
		primary := allSegs[0:3]
		result, err := composeSegmentList(primary, nil)
		r.NoError(err)
		r.Equal(primary, result)
	})

	t.Run("primary is tail subset of replica", func(t *testing.T) {
		// Replica: [0,1,2,3,4,5], Primary: [4,5]
		replica := allSegs[0:6]
		primary := allSegs[4:6]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		r.Equal(replica, result, "should return replica's complete list")
	})

	t.Run("replica is tail subset of primary", func(t *testing.T) {
		// Primary: [0,1,2,3,4,5], Replica: [4,5]
		primary := allSegs[0:6]
		replica := allSegs[4:6]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		r.Equal(primary, result, "should return primary's complete list")
	})

	t.Run("overlapping with merge", func(t *testing.T) {
		// Replica: [0,1,2,3,4,5], Primary: [4,5,6]
		replica := allSegs[0:6]
		primary := allSegs[4:7]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		expected := allSegs[0:7]
		r.Equal(expected, result, "should merge to [0,1,2,3,4,5,6]")
	})

	t.Run("overlapping with merge - single overlap", func(t *testing.T) {
		// Replica: [0,1,2], Primary: [2,3,4]
		replica := allSegs[0:3]
		primary := allSegs[2:5]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		expected := allSegs[0:5]
		r.Equal(expected, result, "should merge to [0,1,2,3,4]")
	})

	t.Run("no overlap - merge and sort", func(t *testing.T) {
		// Replica: [0,1,2], Primary: [3,4,5]
		replica := allSegs[0:3]
		primary := allSegs[3:6]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		// Should merge both lists
		expected := allSegs[0:6]
		r.Equal(expected, result, "should merge and sort all segments")
	})

	t.Run("no overlap - interleaved segments", func(t *testing.T) {
		// Replica: [0,2,4], Primary: [1,3,5]
		replica := []SegmentId{allSegs[0], allSegs[2], allSegs[4]}
		primary := []SegmentId{allSegs[1], allSegs[3], allSegs[5]}
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		// Should merge and sort to [0,1,2,3,4,5]
		expected := allSegs[0:6]
		r.Equal(expected, result, "should merge and sort interleaved segments")
	})

	t.Run("identical lists", func(t *testing.T) {
		primary := allSegs[0:3]
		replica := allSegs[0:3]
		result, err := composeSegmentList(primary, replica)
		r.NoError(err)
		r.Equal(primary, result, "should return either list when identical")
	})

	t.Run("no duplicate segment IDs in result", func(t *testing.T) {
		// Test various scenarios to ensure no duplicates are ever returned
		testCases := []struct {
			name    string
			primary []SegmentId
			replica []SegmentId
		}{
			{
				name:    "identical lists",
				primary: allSegs[0:3],
				replica: allSegs[0:3],
			},
			{
				name:    "overlapping segments",
				primary: allSegs[2:5],
				replica: allSegs[0:3],
			},
			{
				name:    "mixed duplicates",
				primary: []SegmentId{allSegs[0], allSegs[2], allSegs[4]},
				replica: []SegmentId{allSegs[0], allSegs[2], allSegs[5]},
			},
			{
				name:    "all same segment",
				primary: []SegmentId{allSegs[0], allSegs[0], allSegs[0]},
				replica: []SegmentId{allSegs[0], allSegs[0]},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, err := composeSegmentList(tc.primary, tc.replica)
				r.NoError(err)

				// Check for duplicates using a map
				seen := make(map[SegmentId]bool)
				for _, seg := range result {
					r.False(seen[seg], "duplicate segment ID found: %s", seg.String())
					seen[seg] = true
				}
			})
		}
	})
}
