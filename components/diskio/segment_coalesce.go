package diskio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"miren.dev/lbd"
)

// lbd closes a segment on every flush, so a database-like workload makes one
// small segment per fsync, often 50 a second. Uploading each as its own cloud
// object caps a volume's mirror rate at its request rate, and the backlog then
// grows without bound. Consecutive segments of one attachment are instead
// uploaded as one object: their entries copied verbatim under one header,
// labelled with the last segment's label.
//
// The last label keeps every boundary consistent: the local horizon, the
// cloud head and checkpoint boundaries all name the newest segment applied.
// Entries keep their own timestamps and sequences, which replay validates.

// uploadTurnFile pins which segments a turn coalesces until all of them are
// retired. A turn retried after a crash then rebuilds byte-identical objects
// under the same keys, rather than a different grouping the cloud would see
// as a second, overlapping object.
const uploadTurnFile = "accelerator-upload-turn"

// coalesceDir holds coalesced objects while they upload.
const coalesceDir = "upload-staging"

// uploadUnit is one cloud object: a local segment, or a coalesced file of
// several consecutive ones.
type uploadUnit struct {
	path    string
	sources []string
}

func (u uploadUnit) last() string { return u.sources[len(u.sources)-1] }

type segmentScan struct {
	header  lbd.Header
	first   uint64
	count   uint64
	entries []byte
}

// scanSegment reads a closed segment's header and its raw entry items.
func scanSegment(path string) (*segmentScan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := cbor.NewDecoder(bytes.NewReader(data))
	var raw cbor.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	s := &segmentScan{}
	if err := cbor.Unmarshal(raw, &s.header); err != nil {
		return nil, err
	}
	start := dec.NumBytesRead()
	for {
		var e struct {
			Sequence uint64 `cbor:"3,keyasint"`
		}
		err := dec.Decode(&e)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if s.count == 0 {
			s.first = e.Sequence
		} else if e.Sequence != s.first+s.count {
			return nil, fmt.Errorf("non-contiguous sequence")
		}
		s.count++
	}
	s.entries = data[start:]
	return s, nil
}

// coalesceSegments groups consecutive segments of one attachment (same
// header shape, contiguous sequences) and writes each group of two or more
// as one segment file under dir. Anything it cannot parse uploads alone, as
// before. Grouping depends only on the segments, so a retried turn, or its
// unretired suffix, produces the same objects.
func coalesceSegments(dir string, paths []string) ([]uploadUnit, error) {
	var units []uploadUnit
	var group []string
	var scans []*segmentScan
	flush := func() error {
		defer func() { group, scans = nil, nil }()
		if len(group) == 0 {
			return nil
		}
		if len(group) == 1 {
			units = append(units, uploadUnit{path: group[0], sources: group})
			return nil
		}
		label := lbd.LabelFromLogPath(group[len(group)-1])
		header := scans[0].header
		header.SegmentLabel = label
		var buf bytes.Buffer
		if _, err := lbd.NewWriter(&buf, header); err != nil {
			return err
		}
		for _, s := range scans {
			buf.Write(s.entries)
		}
		if err := os.MkdirAll(filepath.Join(dir, coalesceDir), 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, coalesceDir, "disk."+label+".log")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			return err
		}
		units = append(units, uploadUnit{path: path, sources: group})
		return nil
	}
	for _, path := range paths {
		s, err := scanSegment(path)
		if err != nil || s.count == 0 || s.header.SegmentLabel != lbd.LabelFromLogPath(path) {
			if err := flush(); err != nil {
				return nil, err
			}
			units = append(units, uploadUnit{path: path, sources: []string{path}})
			continue
		}
		if len(scans) > 0 {
			prev := scans[len(scans)-1]
			if prev.header.Version != s.header.Version || prev.header.BlockSize != s.header.BlockSize ||
				prev.header.DeviceSize != s.header.DeviceSize || s.first != prev.first+prev.count {
				if err := flush(); err != nil {
					return nil, err
				}
			}
		}
		group = append(group, path)
		scans = append(scans, s)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return units, nil
}

// pinUploadTurn restricts paths to the turn recorded before a crash, if any
// of it is still unretired, and records the turn otherwise.
func pinUploadTurn(diskPath string, paths []string) ([]string, error) {
	file := filepath.Join(diskPath, uploadTurnFile)
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		horizon, err := readLogHorizon(diskPath)
		if err != nil {
			return nil, err
		}
		var pinned []string
		for _, label := range strings.Fields(string(data)) {
			if label > horizon {
				pinned = append(pinned, label)
			}
		}
		if len(pinned) > 0 {
			if len(paths) < len(pinned) {
				return nil, fmt.Errorf("upload turn %v is missing local segments", pinned)
			}
			for i, label := range pinned {
				if lbd.LabelFromLogPath(paths[i]) != label {
					return nil, fmt.Errorf("upload turn %v does not match local segments", pinned)
				}
			}
			return paths[:len(pinned)], nil
		}
	}
	labels := make([]string, len(paths))
	for i, path := range paths {
		labels[i] = lbd.LabelFromLogPath(path)
	}
	return paths, writeRecoveryFile(diskPath, uploadTurnFile, []byte(strings.Join(labels, "\n")))
}
