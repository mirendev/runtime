package diskio

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These assert the wire shapes the cloud actually expects, so a drift on either
// side shows up here rather than as a 400 in production.

func TestCloudUpdatesClientUploadFullFlow(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var (
		begunBody     beginUploadRequestJSON
		uploadedBytes []byte
		completeBody  completeUploadRequestJSON
		beginPath     string
		completePath  string
	)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/updates/upload"):
			beginPath = r.URL.Path
			require.NoError(t, json.NewDecoder(r.Body).Decode(&begunBody))
			json.NewEncoder(w).Encode(beginUploadResponseJSON{
				UpdateID:    "volup-abc",
				UploadURL:   ts.URL + "/upload-target",
				CompleteURL: "/api/v1/disk/volumes/vol-1/updates/volup-abc/complete",
			})
		case r.Method == "PUT" && r.URL.Path == "/upload-target":
			uploadedBytes, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/complete"):
			completePath = r.URL.Path
			require.NoError(t, json.NewDecoder(r.Body).Decode(&completeBody))
			json.NewEncoder(w).Encode(map[string]any{"update_id": "volup-abc"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	payload := []byte("a compressed loop image, notionally")

	updateID, err := client.Upload(context.Background(), "vol-1", UploadRequest{
		Kind:         KindLoopImage,
		OrderingKey:  "0000000000000001",
		SnapshotName: "pre-migration",
		Metadata:     map[string]any{"compression": "zstd"},
	}, bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, err)
	assert.Equal(t, "volup-abc", updateID)

	assert.Equal(t, "/api/v1/disk/volumes/vol-1/updates/upload", beginPath)
	assert.Equal(t, "loop_image", begunBody.Kind)
	assert.Equal(t, "0000000000000001", begunBody.OrderingKey)
	assert.Equal(t, "pre-migration", begunBody.SnapshotName)

	assert.Equal(t, payload, uploadedBytes)

	assert.Equal(t, "/api/v1/disk/volumes/vol-1/updates/volup-abc/complete", completePath)
	assert.Equal(t, int64(len(payload)), completeBody.Size)
	assert.Equal(t, map[string]any{"compression": "zstd"}, completeBody.Metadata)

	// The cloud compares these against the object it received
	sum := md5.Sum(payload)
	assert.Equal(t, base64.StdEncoding.EncodeToString(sum[:]), completeBody.MD5)

	crch := crc32.New(crc32cTable)
	crch.Write(payload)
	assert.Equal(t, base64.StdEncoding.EncodeToString(crch.Sum(nil)), completeBody.CRC32C)
	assert.Equal(t, completeBody.MD5, begunBody.MD5)
	assert.Equal(t, completeBody.CRC32C, begunBody.CRC32C)
	assert.Equal(t, int64(len(payload)), begunBody.Size)
}

func TestCloudUpdatesClientUploadValidatesRequest(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	called := false
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	body := bytes.NewReader([]byte("data"))

	_, err := client.Upload(context.Background(), "vol-1",
		UploadRequest{OrderingKey: "0000000000000001"}, body, 4)
	require.ErrorContains(t, err, "kind is required")

	_, err = client.Upload(context.Background(), "vol-1",
		UploadRequest{Kind: KindLoopImage}, body, 4)
	require.ErrorContains(t, err, "ordering key is required")

	_, err = client.Upload(context.Background(), "vol-1",
		UploadRequest{Kind: KindLoopImage, OrderingKey: "0000000000000001"}, body, 0)
	require.ErrorContains(t, err, "size must be positive")

	assert.False(t, called, "nothing should reach the server for a malformed request")
}

func TestCloudUpdatesClientUploadRejectsForeignCompleteURL(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/updates/upload"):
			json.NewEncoder(w).Encode(beginUploadResponseJSON{
				UpdateID:  "volup-abc",
				UploadURL: ts.URL + "/upload-target",
				// A completion URL pointing somewhere else entirely
				CompleteURL: "https://attacker.example.com/complete",
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.Upload(context.Background(), "vol-1", UploadRequest{
		Kind:        KindLoopImage,
		OrderingKey: "0000000000000001",
	}, bytes.NewReader([]byte("data")), 4)

	require.ErrorContains(t, err, "does not match base URL origin")
}

func TestCloudUpdatesClientUploadSurfacesServerFailure(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"an update already exists for this ordering key"}`))
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.Upload(context.Background(), "vol-1", UploadRequest{
		Kind:        KindLBDLog,
		OrderingKey: "400000000000000100000001",
	}, bytes.NewReader([]byte("data")), 4)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "409")
	assert.Contains(t, err.Error(), "already exists")
}

func TestCloudUpdatesClientListFiltersAndPaginates(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var seenQueries []string
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		seenQueries = append(seenQueries, r.URL.RawQuery)

		// Hand back two pages so the cursor walk is exercised
		if r.URL.Query().Get("cursor") == "" {
			json.NewEncoder(w).Encode(listUpdatesResponseJSON{
				Updates:    []UpdateInfo{{UpdateID: "volup-1", Kind: "lbd_log", OrderingKey: "400000000000000100000001"}},
				NextCursor: "lbd_log:400000000000000100000001",
			})
			return
		}
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates: []UpdateInfo{{UpdateID: "volup-2", Kind: "lbd_log", OrderingKey: "400000000000000100000002"}},
		})
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	updates, err := client.List(context.Background(), "vol-1", ListOptions{
		Kind:  KindLBDLog,
		After: "400000000000000100000000",
	})
	require.NoError(t, err)

	require.Len(t, updates, 2, "both pages should be returned")
	assert.Equal(t, "volup-1", updates[0].UpdateID)
	assert.Equal(t, "volup-2", updates[1].UpdateID)

	require.Len(t, seenQueries, 2)
	assert.Contains(t, seenQueries[0], "kind=lbd_log")
	assert.Contains(t, seenQueries[0], "after=400000000000000100000000")
	assert.Contains(t, seenQueries[1], "cursor=lbd_log")
}

// The cloud refuses after/until without a kind; catching it locally gives a
// clearer message than a 400.
func TestCloudUpdatesClientListRequiresKindWithAfter(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	called := false
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.List(context.Background(), "vol-1", ListOptions{After: "0000000000000001"})

	require.ErrorContains(t, err, "requires a kind")
	assert.False(t, called)
}

func TestCloudUpdatesClientDownload(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)
	payload := []byte("the update payload")

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/download"):
			assert.Equal(t, "/api/v1/disk/volumes/vol-1/updates/volup-abc/download", r.URL.Path)
			json.NewEncoder(w).Encode(downloadResponseJSON{DownloadURL: ts.URL + "/blob"})
		case r.URL.Path == "/blob":
			// A presigned URL carries its own authorization
			assert.Empty(t, r.Header.Get("Authorization"))
			w.Write(payload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	rc, err := client.Download(context.Background(), "vol-1", "volup-abc")
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestCloudUpdatesClientDownloadSurfacesMissingUpdate(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"Update not found"}`))
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.Download(context.Background(), "vol-1", "volup-missing")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// A server that repeats a cursor would otherwise spin this forever, growing the
// result slice until the process dies. Callers pass context.Background(), so
// nothing else would stop it.
func TestCloudUpdatesClientListRefusesRepeatedCursor(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	calls := 0
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates:    []UpdateInfo{{UpdateID: "volup-1", Kind: "lbd_log", OrderingKey: "40000000682f1a2c1dcd6500"}},
			NextCursor: "lbd_log:stuck",
		})
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.List(context.Background(), "vol-1", ListOptions{})

	require.ErrorContains(t, err, "repeated cursor")
	assert.LessOrEqual(t, calls, 3, "should give up as soon as the cursor repeats")
}

func TestCloudUpdatesClientListStopsAtPageCeiling(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	page := 0
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		page++
		// Always a fresh cursor, so only the ceiling ends this
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates:    []UpdateInfo{{UpdateID: fmt.Sprintf("volup-%d", page), Kind: "lbd_log"}},
			NextCursor: fmt.Sprintf("lbd_log:%d", page),
		})
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.List(context.Background(), "vol-1", ListOptions{})

	require.ErrorContains(t, err, "more than")
	assert.LessOrEqual(t, page, maxUpdateListPages+1)
}

// Restore asks for one row from the newest end. The point of the option is to
// avoid the page walk, so the test that matters is that it stops after one
// request even when the server offers a cursor to keep going.
func TestCloudUpdatesClientListDescendingStopsAtLimit(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var seenQueries []string
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		seenQueries = append(seenQueries, r.URL.RawQuery)
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates:    []UpdateInfo{{UpdateID: "volup-newest", Kind: "loop_image", OrderingKey: "0000000000000009"}},
			NextCursor: "loop_image:0000000000000009",
		})
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	updates, err := client.List(context.Background(), "vol-1", ListOptions{
		Kind:       KindLoopImage,
		Descending: true,
		Limit:      1,
	})
	require.NoError(t, err)

	require.Len(t, updates, 1)
	assert.Equal(t, "volup-newest", updates[0].UpdateID)

	require.Len(t, seenQueries, 1, "a bounded list must not follow the cursor")
	assert.Contains(t, seenQueries[0], "order=desc")
	assert.Contains(t, seenQueries[0], "limit=1")
}

// Without a limit the walk is unchanged, so replay keeps getting everything.
func TestCloudUpdatesClientListStaysAscendingByDefault(t *testing.T) {
	ts, h, authClient := newTestUploaderServer(t)

	var seenQueries []string
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		seenQueries = append(seenQueries, r.URL.RawQuery)
		json.NewEncoder(w).Encode(listUpdatesResponseJSON{
			Updates: []UpdateInfo{{UpdateID: "volup-1", Kind: "loop_image", OrderingKey: "0000000000000001"}},
		})
	}

	client := NewCloudUpdatesClient(slog.Default(), ts.URL, authClient)
	_, err := client.List(context.Background(), "vol-1", ListOptions{Kind: KindLoopImage})
	require.NoError(t, err)

	require.Len(t, seenQueries, 1)
	assert.NotContains(t, seenQueries[0], "order=")
	assert.NotContains(t, seenQueries[0], "limit=")
}

func TestCloudUpdatesClientRejectsUnmatchedProof(t *testing.T) {
	for _, state := range []string{"already_active", "prepared"} {
		for _, field := range []string{"kind", "ordering_key", "md5", "crc32c", "size"} {
			t.Run(state+"/"+field, func(t *testing.T) {
				ts, h, auth := newTestUploaderServer(t)
				h.handler = func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/updates/upload") {
						t.Error("mismatched claim must not upload or complete")
						http.Error(w, "unexpected request", http.StatusBadRequest)
						return
					}
					var claim map[string]any
					require.NoError(t, json.NewDecoder(r.Body).Decode(&claim))
					claim["update_id"], claim[state] = "volup-existing", true
					if field == "size" {
						claim[field] = 99
					} else {
						claim[field] = "different"
					}
					json.NewEncoder(w).Encode(claim)
				}
				client := NewCloudUpdatesClient(slog.Default(), ts.URL, auth)
				_, err := client.Upload(context.Background(), "vol-test", UploadRequest{Kind: KindLBDLog, OrderingKey: "400000000000000100000001"}, bytes.NewReader([]byte("payload")), 7)
				require.ErrorContains(t, err, "update does not match local payload")
			})
		}
	}
}

func TestCloudPreparationFailsClosedOnOldReplica(t *testing.T) {
	for _, capability := range []bool{false, true} {
		t.Run(fmt.Sprint(capability), func(t *testing.T) {
			ts, h, auth := newTestUploaderServer(t)
			published := false
			h.handler = func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/updates/upload"):
					json.NewEncoder(w).Encode(map[string]any{"update_id": "volup-old", "upload_url": ts.URL + "/put", "supports_prepare": capability})
				case r.Method == "PUT":
					io.Copy(io.Discard, r.Body)
				case strings.HasSuffix(r.URL.Path, "/complete"):
					published = true // old handler ignores any prepare_only field
				default:
					http.NotFound(w, r)
				}
			}
			client := NewCloudUpdatesClient(slog.Default(), ts.URL, auth)
			u := NewCloudSegmentUploaderWithClient(slog.Default(), client, nil)
			err := u.UploadSegments(context.Background(), "vol-test", t.TempDir(), []string{writeTestSegment(t, "400000000000000100000001", []byte("data"))}, func(string) error { t.Error("must not retire"); return nil })
			require.Error(t, err)
			require.False(t, published)
		})
	}
}
