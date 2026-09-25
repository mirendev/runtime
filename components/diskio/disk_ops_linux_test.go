package diskio

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLoopBacking(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want LoopBacking
	}{
		{
			name: "a live backing file",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/disk.img\n",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/disk.img"},
		},
		{
			// What the kernel reports once the file has lost its last name.
			// The path is where it was when that happened.
			name: "a backing file that has been unlinked",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/disk.img (deleted)\n",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/disk.img", Deleted: true},
		},
		{
			// A renamed file is followed to its new name with no marker: the
			// kernel tracks the inode, and the inode still has a name. Only
			// losing its last name marks it.
			name: "a backing file that was renamed rather than removed",
			raw:  "/var/lib/miren/disk-data/volumes/vol-1/renamed.img",
			want: LoopBacking{Path: "/var/lib/miren/disk-data/volumes/vol-1/renamed.img"},
		},
		{
			name: "an unlinked file whose own name ended in the marker",
			raw:  "/tmp/weird (deleted) (deleted)",
			want: LoopBacking{Path: "/tmp/weird (deleted)", Deleted: true},
		},
		{
			// Genuinely ambiguous — a live file named "weird (deleted)" reads
			// identically to a deleted file named "weird", and the kernel gives
			// no way to tell. Read as deleted, which is the safe way to be
			// wrong: it costs an extra loop device rather than stale data.
			name: "the ambiguous case is read as deleted",
			raw:  "/tmp/weird (deleted)",
			want: LoopBacking{Path: "/tmp/weird", Deleted: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, parseLoopBacking(tc.raw))
		})
	}
}
