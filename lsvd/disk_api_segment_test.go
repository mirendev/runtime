package lsvd

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskAPISegmentAccess_ImplementsInterface(t *testing.T) {
	logger := slog.Default()
	access := NewDiskAPISegmentAccess(logger, "http://localhost:8080", "test-token")

	var _ SegmentAccess = access
}

func TestDiskAPISegmentAccess_WithMockServer_UploadFlow_AbsoluteURL(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Mock server responses
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock external upload endpoint
		if r.Method == "PUT" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer uploadServer.Close()

	completeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock complete endpoint
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/complete") {
			response := map[string]any{
				"segment_id": "lsvd_123456789",
				"status":     "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		w.WriteHeader(404)
	}))
	defer completeServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/disk/segments/upload":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}

			// Return absolute URL for completed_url
			response := map[string]any{
				"segment_id":    "lsvd_123456789",
				"upload_url":    uploadServer.URL,
				"completed_url": completeServer.URL + "/api/v1/disk/segments/lsvd_123456789/complete", // Absolute URL
				"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")

	// Test that we can create a volume object
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)
	require.NotNil(t, vol)

	// Test upload with a real file
	tmpFile, err := os.CreateTemp("", "test-segment")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	testData := "Hello, this is test segment data!"
	_, err = tmpFile.WriteString(testData)
	require.NoError(t, err)

	segId := SegmentId{}
	layout := &SegmentLayout{}

	err = vol.NewSegment(ctx, segId, layout, tmpFile)
	assert.NoError(t, err, "NewSegment should work with absolute completed_url")
}

func TestDiskAPISegmentAccess_WithMockServer_UploadFlow(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Mock server responses
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock external upload endpoint
		if r.Method == "PUT" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer uploadServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/disk/segments/upload":
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}

			// Verify the request includes lsvd_id
			var uploadReq SegmentUploadRequest
			if err := json.NewDecoder(r.Body).Decode(&uploadReq); err != nil {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
				return
			}
			if uploadReq.LsvdID == "" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "lsvd_id is required"})
				return
			}

			response := map[string]any{
				"segment_id":    uploadReq.LsvdID, // Use the provided lsvd_id
				"upload_url":    uploadServer.URL,
				"completed_url": "/api/v1/disk/segments/" + uploadReq.LsvdID + "/complete",
				"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/segments/") && strings.HasSuffix(r.URL.Path, "/complete"):
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}

			response := map[string]any{
				"segment_id": "lsvd_123456789",
				"status":     "completed",
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")

	// Test that we can create a volume object
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)
	require.NotNil(t, vol)

	// Test upload with a real file
	tmpFile, err := os.CreateTemp("", "test-segment")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	testData := "Hello, this is test segment data!"
	_, err = tmpFile.WriteString(testData)
	require.NoError(t, err)

	segId := SegmentId{}
	layout := &SegmentLayout{}

	err = vol.NewSegment(ctx, segId, layout, tmpFile)
	assert.NoError(t, err, "NewSegment should work with mock server")
}

func TestDiskAPISegmentAccess_WithMockServer_DownloadFlow(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Mock server for download functionality
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/disk/segments/") && strings.HasSuffix(r.URL.Path, "/download") {
			if r.Method != "GET" {
				w.WriteHeader(405)
				return
			}

			response := map[string]any{
				"download_url": "https://storage.example.com/download",
				"expires_at":   time.Now().Add(time.Hour).Format(time.RFC3339),
				"size":         1048576,
				"md5":          "098f6bcd4621d373cade4e832627b4f6",
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)

	segId := SegmentId{}
	reader, err := vol.OpenSegment(ctx, segId)
	assert.NoError(t, err, "OpenSegment should work with mock server")
	assert.NotNil(t, reader)
}

func TestDiskAPISegmentAccess_VolumeOperations_WithMockServer(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Create a segments mock server first
	segmentsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return actual segments list as JSON array
		segments := []string{
			"01HQZP3K4K9XQJX8WB6QR5NTEA",
			"01HQZP3K4K9XQJX8WB6QR5NTEB",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(segments)
	}))
	defer segmentsServer.Close()

	// Mock server for volume operations
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/disk/volumes" && r.Method == "POST":
			// Volume creation
			response := map[string]any{
				"volume_id":  "vol_abc123",
				"version_id": "volrev_xyz789",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case r.URL.Path == "/api/v1/disk/volumes" && r.Method == "GET":
			// Volume listing
			response := map[string]any{
				"volumes": []map[string]any{
					{
						"volume_id":     "vol_abc123",
						"name":          "test-volume",
						"uuid":          "550e8400-e29b-41d4-a716-446655440000",
						"declared_size": 1073741824,
						"data_format":   "ext4",
						"app_format":    "postgresql",
						"created_at":    time.Now().Format(time.RFC3339),
						"updated_at":    time.Now().Format(time.RFC3339),
					},
				},
				"next_cursor": "vol_xyz789",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && strings.HasSuffix(r.URL.Path, "/latest/segments") && r.Method == "GET":
			// Direct latest segments endpoint - return URL to mock segments endpoint
			response := map[string]any{
				"url": segmentsServer.URL,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && strings.HasSuffix(r.URL.Path, "/latest") && r.Method == "GET":
			// Volume latest version info
			response := map[string]any{
				"version_id":   "volrev_xyz789",
				"volume_id":    "vol_abc123",
				"block_size":   4096,
				"total_blocks": 262144,
				"created_at":   time.Now().Format(time.RFC3339),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && r.Method == "GET":
			// Direct volume info endpoint (must be after more specific endpoints)
			volumeID := strings.TrimPrefix(r.URL.Path, "/api/v1/disk/volumes/")
			if volumeID == "test-volume" || volumeID == "vol_abc123" {
				response := map[string]any{
					"volume_id":     "vol_abc123",
					"name":          "test-volume",
					"uuid":          "550e8400-e29b-41d4-a716-446655440000",
					"declared_size": 1073741824,
					"data_format":   "ext4",
					"app_format":    "postgresql",
					"created_at":    time.Now().Format(time.RFC3339),
					"updated_at":    time.Now().Format(time.RFC3339),
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			} else {
				w.WriteHeader(404)
				json.NewEncoder(w).Encode(map[string]string{"error": "Volume not found"})
			}

		case strings.Contains(r.URL.Path, "/versions/") && strings.HasSuffix(r.URL.Path, "/segments") && r.Method == "GET":
			// Volume segments list - return URL to mock segments endpoint
			response := map[string]any{
				"url": segmentsServer.URL,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")

	t.Run("InitVolume", func(t *testing.T) {
		volInfo := &VolumeInfo{
			Name: "test-volume",
			Size: 1073741824,
			UUID: "550e8400-e29b-41d4-a716-446655440000",
		}

		err := access.InitVolume(ctx, volInfo)
		assert.NoError(t, err, "InitVolume should work with mock server")
	})

	t.Run("ListVolumes", func(t *testing.T) {
		volumes, err := access.ListVolumes(ctx)
		assert.NoError(t, err, "ListVolumes should work with mock server")
		assert.Len(t, volumes, 1)
		assert.Equal(t, "test-volume", volumes[0])
	})

	t.Run("GetVolumeInfo", func(t *testing.T) {
		info, err := access.GetVolumeInfo(ctx, "vol_abc123")
		assert.NoError(t, err, "GetVolumeInfo should work with mock server")
		assert.NotNil(t, info)
	})
}

func TestDiskAPIVolume_VolumeOperations_WithMockServer(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Create a segments mock server first
	segmentsServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return actual segments list as JSON array
		segments := []string{
			"01HQZP3K4K9XQJX8WB6QR5NTEC",
			"01HQZP3K4K9XQJX8WB6QR5NTED",
			"01HQZP3K4K9XQJX8WB6QR5NTEE",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(segments)
	}))
	defer segmentsServer2.Close()

	// Mock server for volume-level operations
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/disk/volumes" && r.Method == "GET":
			// Volume listing (needed for Info() method)
			response := map[string]any{
				"volumes": []map[string]any{
					{
						"volume_id":     "vol_abc123",
						"name":          "test-volume",
						"uuid":          "550e8400-e29b-41d4-a716-446655440000",
						"declared_size": 1073741824,
						"created_at":    time.Now().Format(time.RFC3339),
						"updated_at":    time.Now().Format(time.RFC3339),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && strings.HasSuffix(r.URL.Path, "/latest/segments") && r.Method == "GET":
			// Direct latest segments endpoint - return 302 redirect
			w.Header().Set("Location", segmentsServer2.URL)
			w.Header().Set("X-Volume-Version-Id", "volrev_xyz789")
			w.WriteHeader(http.StatusFound)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && strings.HasSuffix(r.URL.Path, "/latest") && r.Method == "GET":
			// Volume latest version info
			response := map[string]any{
				"version_id":   "volrev_xyz789",
				"volume_id":    "vol_abc123",
				"block_size":   4096,
				"total_blocks": 262144,
				"created_at":   time.Now().Format(time.RFC3339),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasPrefix(r.URL.Path, "/api/v1/disk/volumes/") && r.Method == "GET":
			// Direct volume info endpoint
			volumeID := strings.TrimPrefix(r.URL.Path, "/api/v1/disk/volumes/")
			if volumeID == "test-volume" || volumeID == "vol_abc123" {
				response := map[string]any{
					"volume_id":     "vol_abc123",
					"name":          "test-volume",
					"uuid":          "550e8400-e29b-41d4-a716-446655440000",
					"declared_size": 1073741824,
					"created_at":    time.Now().Format(time.RFC3339),
					"updated_at":    time.Now().Format(time.RFC3339),
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			} else {
				w.WriteHeader(404)
			}

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)

	t.Run("Info", func(t *testing.T) {
		info, err := vol.Info(ctx)
		assert.NoError(t, err, "Volume Info should work with mock server")
		assert.NotNil(t, info)
	})

	t.Run("ListSegments", func(t *testing.T) {
		segments, err := vol.ListSegments(ctx)
		assert.NoError(t, err, "Volume ListSegments should work with mock server")
		assert.NotNil(t, segments)
		// Should have the segments returned by the mock server
		assert.Len(t, segments, 3, "Should have 3 segments from mock server")

		// Verify the actual segment IDs match
		expectedIDs := []string{
			"01HQZP3K4K9XQJX8WB6QR5NTEC",
			"01HQZP3K4K9XQJX8WB6QR5NTED",
			"01HQZP3K4K9XQJX8WB6QR5NTEE",
		}
		for i, seg := range segments {
			assert.Equal(t, expectedIDs[i], seg.String(), "Segment ID should match")
		}
	})
}

func TestDiskAPISegmentReader_URLRefresh(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Track how many times download endpoint is called
	downloadCallCount := 0

	// Create a file data server first
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return some data for range requests
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write([]byte("test data for segment that is longer than we need for testing"))
	}))
	defer fileServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") && r.Method == "GET" {
			downloadCallCount++

			// Return a URL that expires in an hour (typical)
			response := map[string]any{
				"download_url": fileServer.URL,
				"expires_at":   time.Now().Add(60 * time.Minute).Format(time.RFC3339), // Standard 60 min expiry
				"size":         1024,
				"md5":          "098f6bcd4621d373cade4e832627b4f6",
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)

	segId, err := ParseSegment("01HQZP3K4K9XQJX8WB6QR5NTEZ")
	require.NoError(t, err)

	reader, err := vol.OpenSegment(ctx, segId)
	require.NoError(t, err)
	defer reader.Close()

	// Cast to our concrete type to manipulate expiry
	diskReader, ok := reader.(*DiskAPISegmentReader)
	require.True(t, ok, "Should be DiskAPISegmentReader")

	// Initial read should work
	buf := make([]byte, 10)
	n, err := reader.ReadAt(buf, 0)
	assert.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, 1, downloadCallCount, "Should have called download once initially")

	// Force expiry and read again
	diskReader.mu.Lock()
	diskReader.expiresAt = time.Now().Add(-time.Minute) // Expired
	diskReader.mu.Unlock()

	n, err = reader.ReadAt(buf, 10)
	assert.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, 2, downloadCallCount, "Should have refreshed the URL")

	// Another read with non-expired URL shouldn't refresh
	_, err = reader.ReadAt(buf, 20)
	assert.NoError(t, err)
	assert.Equal(t, 2, downloadCallCount, "Should not have refreshed again")
}

func TestDiskAPISegmentAccess_Checksums(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Variables to capture the checksums sent to complete endpoint
	var receivedMD5 string
	var receivedCRC32C string
	var receivedSize int64

	// Expected values for our test data
	testData := "Hello, this is test segment data!"
	expectedSize := int64(len(testData))
	// Pre-calculated MD5 of testData: "Hello, this is test segment data!"
	expectedMD5 := "FdQsw2ChdtoOXLZvpLkuUA==" // base64 encoded
	// Pre-calculated CRC32C (Castagnoli) of testData
	expectedCRC32C := "227Ejw==" // base64 encoded (different from IEEE which would be "Wf7SmQ==")

	// Mock server responses
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock external upload endpoint
		if r.Method == "PUT" {
			// Read the uploaded data to ensure checksums are calculated
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, testData, string(body), "Uploaded data should match")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer uploadServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/disk/segments/upload" && r.Method == "POST":
			response := map[string]any{
				"segment_id":    "lsvd_checksum_test",
				"upload_url":    uploadServer.URL,
				"completed_url": "/api/v1/disk/segments/lsvd_checksum_test/complete",
				"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasSuffix(r.URL.Path, "/complete") && r.Method == "POST":
			// Capture the checksums from the complete request
			var completeReq CompleteUploadRequest
			err := json.NewDecoder(r.Body).Decode(&completeReq)
			require.NoError(t, err)

			receivedMD5 = completeReq.MD5
			receivedCRC32C = completeReq.CRC32C
			receivedSize = completeReq.Size

			response := map[string]any{
				"segment_id": "lsvd_checksum_test",
				"status":     "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)

	// Create test file with known content
	tmpFile, err := os.CreateTemp("", "test-segment-checksum")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err = tmpFile.WriteString(testData)
	require.NoError(t, err)

	segId := SegmentId{}
	layout := &SegmentLayout{}

	err = vol.NewSegment(ctx, segId, layout, tmpFile)
	require.NoError(t, err, "NewSegment should succeed")

	// Verify the checksums were calculated and sent correctly
	assert.Equal(t, expectedMD5, receivedMD5, "MD5 checksum should match")
	assert.Equal(t, expectedCRC32C, receivedCRC32C, "CRC32C checksum should match")
	assert.Equal(t, expectedSize, receivedSize, "Size should match")
}

func TestDiskAPISegmentAccess_LargeFileChecksums(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Variables to capture the checksums
	var receivedMD5 string
	var receivedCRC32C string
	var receivedSize int64

	// Create a larger test file (1MB)
	testSize := 1024 * 1024 // 1MB
	testData := make([]byte, testSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	// Calculate expected checksums
	md5Hash := md5.New()
	crc32cTable := crc32.MakeTable(crc32.Castagnoli)
	crcHash := crc32.New(crc32cTable)
	md5Hash.Write(testData)
	crcHash.Write(testData)
	expectedMD5 := base64.StdEncoding.EncodeToString(md5Hash.Sum(nil))
	expectedCRC32C := base64.StdEncoding.EncodeToString(crcHash.Sum(nil))
	expectedSize := int64(testSize)

	// Mock servers
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			// Verify we receive all the data
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.Equal(t, testSize, len(body), "Upload size should match")
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer uploadServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/disk/segments/upload" && r.Method == "POST":
			response := map[string]any{
				"segment_id":    "lsvd_large_test",
				"upload_url":    uploadServer.URL,
				"completed_url": "/api/v1/disk/segments/lsvd_large_test/complete",
				"expires_at":    time.Now().Add(time.Hour).Format(time.RFC3339),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case strings.HasSuffix(r.URL.Path, "/complete") && r.Method == "POST":
			// Capture checksums
			var completeReq CompleteUploadRequest
			err := json.NewDecoder(r.Body).Decode(&completeReq)
			require.NoError(t, err)

			receivedMD5 = completeReq.MD5
			receivedCRC32C = completeReq.CRC32C
			receivedSize = completeReq.Size

			response := map[string]any{
				"segment_id": "lsvd_large_test",
				"status":     "completed",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")
	vol, err := access.OpenVolume(ctx, "test-volume")
	require.NoError(t, err)

	// Create large test file
	tmpFile, err := os.CreateTemp("", "test-large-segment")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err = tmpFile.Write(testData)
	require.NoError(t, err)

	segId := SegmentId{}
	layout := &SegmentLayout{}

	err = vol.NewSegment(ctx, segId, layout, tmpFile)
	require.NoError(t, err, "NewSegment should succeed with large file")

	// Verify checksums
	assert.Equal(t, expectedMD5, receivedMD5, "MD5 checksum should match for large file")
	assert.Equal(t, expectedCRC32C, receivedCRC32C, "CRC32C checksum should match for large file")
	assert.Equal(t, expectedSize, receivedSize, "Size should match for large file")
}

func TestDiskAPISegmentAccess_SegmentDeletion_WithMockServer(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()

	// Mock server for segment deletion
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/disk/segments/") && r.Method == "DELETE" {
			// Extract segment ID from path
			pathParts := strings.Split(r.URL.Path, "/")
			segmentID := pathParts[len(pathParts)-1]

			response := map[string]any{
				"lsvd_id": segmentID,
				"status":  "deleted",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else {
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	access := NewDiskAPISegmentAccess(logger, server.URL, "test-token")

	t.Run("RemoveSegment via SegmentAccess", func(t *testing.T) {
		segId, err := ParseSegment("01HQZP3K4K9XQJX8WB6QR5NTEZ")
		require.NoError(t, err)

		err = access.RemoveSegment(ctx, segId)
		assert.NoError(t, err, "RemoveSegment should work with mock server")
	})

	t.Run("RemoveSegment via Volume", func(t *testing.T) {
		vol, err := access.OpenVolume(ctx, "test-volume")
		require.NoError(t, err)

		segId, err := ParseSegment("01HQZP3K4K9XQJX8WB6QR5NTEZ")
		require.NoError(t, err)

		err = vol.RemoveSegment(ctx, segId)
		assert.NoError(t, err, "Volume RemoveSegment should work with mock server")
	})

	t.Run("RemoveSegment handles errors", func(t *testing.T) {
		// Use invalid server URL to test error handling
		badAccess := NewDiskAPISegmentAccess(logger, "http://invalid-server:99999", "test-token")

		segId, err := ParseSegment("01HQZP3K4K9XQJX8WB6QR5NTEZ")
		require.NoError(t, err)

		err = badAccess.RemoveSegment(ctx, segId)
		assert.Error(t, err, "RemoveSegment should fail with invalid server")
		assert.Contains(t, err.Error(), "failed to delete segment")
	})
}
