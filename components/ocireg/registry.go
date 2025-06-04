package ocireg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
)

// RegistryHandler processes all OCI registry requests
type Registry struct {
	RootDir string `asm:"data-path"`
	Log     *slog.Logger
	EC      *entityserver.Client

	server *http.Server
}

func (r *Registry) Populated() error {
	return nil
}

func (r *Registry) Start(ctx context.Context, addr string) error {
	path := filepath.Join(r.RootDir, "registry")

	// Create storage directory if it doesn't exist
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	// Create registry handler
	registry := NewRegistryHandler(path, r.Log, r.EC)

	mux := http.NewServeMux()

	// Set up HTTP server
	mux.Handle("/v2/", registry)

	// Add basic health check endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "OK",
			"message": "OCI Registry is running",
		})
	})

	r.Log.Info("Starting OCI Registry", "addr", addr, "path", path)

	r.server = &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	go r.server.ListenAndServe()
	return nil
}

func (r *Registry) Shutdown(ctx context.Context) error {
	if r.server == nil {
		return fmt.Errorf("shutdown called but server is not running")
	}
	return r.server.Shutdown(ctx)
}

type RegistryHandler struct {
	storageRoot string
	log         *slog.Logger
	ec          *entityserver.Client
}

// NewRegistryHandler creates a new registry handler with the specified storage location
func NewRegistryHandler(storageRoot string, log *slog.Logger, ec *entityserver.Client) *RegistryHandler {
	return &RegistryHandler{
		storageRoot: storageRoot,
		log:         log,
		ec:          ec,
	}
}

// ServeHTTP implements the http.Handler interface
func (h *RegistryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the leading /v2/ from the path
	path := strings.TrimPrefix(r.URL.Path, "/v2/")

	// Basic API version check endpoint
	if path == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Split the path into components
	parts := strings.Split(path, "/")

	// Handle API endpoints based on the URL pattern
	switch {
	// /v2/<name>/manifests/<reference>
	case len(parts) >= 3 && parts[len(parts)-2] == "manifests":
		name := strings.Join(parts[:len(parts)-2], "/")
		reference := parts[len(parts)-1]

		if r.Method == http.MethodGet {
			h.getManifest(w, r, name, reference)
		} else if r.Method == http.MethodPut {
			h.putManifest(w, r, name, reference)
		} else if r.Method == http.MethodHead {
			h.headManifest(w, r, name, reference)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/blobs/<digest>
	case len(parts) >= 3 && parts[len(parts)-2] == "blobs":
		name := strings.Join(parts[:len(parts)-2], "/")
		digest := parts[len(parts)-1]

		if r.Method == http.MethodGet {
			h.getBlob(w, r, name, digest)
		} else if r.Method == http.MethodHead {
			h.headBlob(w, r, name, digest)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/blobs/uploads/
	case len(parts) >= 3 && parts[len(parts)-2] == "blobs" && parts[len(parts)-1] == "uploads":
		name := strings.Join(parts[:len(parts)-2], "/")

		if r.Method == http.MethodPost {
			h.initBlobUpload(w, r, name)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/blobs/uploads/<uuid>
	case len(parts) >= 4 && parts[len(parts)-3] == "blobs" && parts[len(parts)-2] == "uploads":
		name := strings.Join(parts[:len(parts)-3], "/")
		uuid := parts[len(parts)-1]

		if uuid == "" && r.Method == http.MethodPost {
			h.initBlobUpload(w, r, name)
		} else if r.Method == http.MethodPut {
			h.completeBlobUpload(w, r, name, uuid)
		} else if r.Method == http.MethodPatch {
			h.chunkBlobUpload(w, r, name, uuid)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// getManifest handles GET requests for manifests
func (h *RegistryHandler) getManifest(w http.ResponseWriter, r *http.Request, name, reference string) {
	var artifact core_v1alpha.Artifact

	if strings.HasPrefix(reference, "sha256:") {
		err := h.ec.OneAtIndex(r.Context(), entity.String(core_v1alpha.ArtifactManifestDigestId, reference), &artifact)
		if err != nil {
			h.log.Error("Error getting artifact by digest", "digest", reference, "error", err)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		h.log.Info("Found app version by digest", "digest", reference, "appVer", artifact.ID)
	} else {
		err := h.ec.Get(r.Context(), reference, &artifact)
		if err != nil {
			h.log.Error("Error getting artifact", "reference", reference, "error", err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}

	data := []byte(artifact.Manifest)

	/*
		manifestPath := filepath.Join(h.storageRoot, "manifests", name, reference)

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Printf("Error reading manifest %s: %v", manifestPath, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
	*/

	sum := sha256.Sum256(data)

	// Set the content type based on the manifest type (usually application/vnd.oci.image.manifest.v1+json)
	// For simplicity, we'll set a default OCI content type
	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(sum[:]))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// headManifest handles HEAD requests for manifests
func (h *RegistryHandler) headManifest(w http.ResponseWriter, r *http.Request, name, reference string) {
	var appVer core_v1alpha.Artifact

	err := h.ec.Get(r.Context(), reference, &appVer)
	if err != nil {
		h.log.Error("Error getting artifact", "reference", reference, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	data := []byte(appVer.Manifest)
	/*
		manifestPath := filepath.Join(h.storageRoot, "manifests", name, reference)

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			log.Printf("Error reading manifest %s: %v", manifestPath, err)
			w.WriteHeader(http.StatusNotFound)
			return
		}
	*/

	sum := sha256.Sum256(data)

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(sum[:]))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
}

// putManifest handles PUT requests for manifests
func (h *RegistryHandler) putManifest(w http.ResponseWriter, r *http.Request, name, reference string) {
	// Read the manifest data
	manifestData, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Error("Error reading manifest data", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sum := sha256.Sum256(manifestData)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	// Set the digest header

	var (
		app      core_v1alpha.App
		artifact core_v1alpha.Artifact
	)

	artifact.Manifest = string(manifestData)
	artifact.ManifestDigest = digest

	err = h.ec.Get(r.Context(), name, &app)
	if err != nil {
		h.log.Error("Error getting app during artifact creation, will create orphan artifact", "name", name, "error", err)
	} else {
		artifact.App = app.ID
	}

	artId, err := h.ec.Create(r.Context(), reference, &artifact)
	if err != nil {
		h.log.Error("Error creating app version", "name", name, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	h.log.Info("Created new artifact", "name", name, "reference", reference, "id", artId)

	/*
		// Create the directory structure if it doesn't exist
		manifestDir := filepath.Join(h.storageRoot, "manifests", name)
		err = os.MkdirAll(manifestDir, 0755)
		if err != nil {
			log.Printf("Error creating manifest directory %s: %v", manifestDir, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Write the manifest file
		manifestPath := filepath.Join(manifestDir, reference)
		err = os.WriteFile(manifestPath, manifestData, 0644)
		if err != nil {
			log.Printf("Error writing manifest %s: %v", manifestPath, err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	*/

	// Set the digest header
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// getBlob handles GET requests for blobs
func (h *RegistryHandler) getBlob(w http.ResponseWriter, r *http.Request, name, digest string) {
	blobPath := filepath.Join(h.storageRoot, "blobs", digest)

	data, err := os.ReadFile(blobPath)
	if err != nil {
		h.log.Error("Error reading blob", "blobPath", blobPath, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// headBlob handles HEAD requests for blobs
func (h *RegistryHandler) headBlob(w http.ResponseWriter, r *http.Request, name, digest string) {
	blobPath := filepath.Join(h.storageRoot, "blobs", digest)

	fileInfo, err := os.Stat(blobPath)
	if err != nil {
		h.log.Error("Error stating blob", "blobPath", blobPath, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.WriteHeader(http.StatusOK)
}

// initBlobUpload handles POST requests to initialize blob uploads
func (h *RegistryHandler) initBlobUpload(w http.ResponseWriter, r *http.Request, name string) {
	// Generate a unique ID for this upload
	uploadID := idgen.Gen("b")

	// Create the upload directory if it doesn't exist
	uploadDir := filepath.Join(h.storageRoot, "uploads")
	err := os.MkdirAll(uploadDir, 0755)
	if err != nil {
		h.log.Error("Error creating upload directory", "uploadDir", uploadDir, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Create an empty upload file
	uploadPath := filepath.Join(uploadDir, uploadID)
	err = os.WriteFile(uploadPath, []byte{}, 0644)
	if err != nil {
		h.log.Error("Error creating upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set the Location header for the client to use for subsequent upload chunks
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, uploadID))
	w.Header().Set("Range", "0-0")
	w.Header().Set("Docker-Upload-UUID", uploadID)
	w.WriteHeader(http.StatusAccepted)
}

// chunkBlobUpload handles PATCH requests to upload blob chunks
func (h *RegistryHandler) chunkBlobUpload(w http.ResponseWriter, r *http.Request, name, uuid string) {
	uploadPath := filepath.Join(h.storageRoot, "uploads", uuid)

	// Check if the upload exists
	_, err := os.Stat(uploadPath)
	if err != nil {
		h.log.Error("Error stating upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Open the file in append mode
	file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		h.log.Error("Error opening upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Get current file size
	fileInfo, err := file.Stat()
	if err != nil {
		h.log.Error("Error stating upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	startRange := fileInfo.Size()

	// Copy request body to the file
	n, err := io.Copy(file, r.Body)
	if err != nil {
		h.log.Error("Error copying data to upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set headers for the response
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, uuid))
	w.Header().Set("Range", fmt.Sprintf("0-%d", startRange+n-1))
	w.Header().Set("Docker-Upload-UUID", uuid)
	w.WriteHeader(http.StatusAccepted)
}

// completeBlobUpload handles PUT requests to complete blob uploads
func (h *RegistryHandler) completeBlobUpload(w http.ResponseWriter, r *http.Request, name, uuid string) {
	uploadPath := filepath.Join(h.storageRoot, "uploads", uuid)

	// Check if the upload exists
	_, err := os.Stat(uploadPath)
	if err != nil {
		h.log.Error("Error stating upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Get the digest from the query parameters
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		h.log.Error("Missing digest parameter", "uploadPath", uploadPath)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Create the blobs directory if it doesn't exist
	blobsDir := filepath.Join(h.storageRoot, "blobs")
	err = os.MkdirAll(blobsDir, 0755)
	if err != nil {
		h.log.Error("Error creating blobs directory", "blobsDir", blobsDir, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// If there's data in the request body, append it to the upload
	if r.ContentLength > 0 {
		file, err := os.OpenFile(uploadPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			h.log.Error("Error opening upload file", "uploadPath", uploadPath, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = io.Copy(file, r.Body)
		if err != nil {
			file.Close()
			h.log.Error("Error copying data to upload file", "uploadPath", uploadPath, "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		file.Close()
	}

	// Move the upload to the final location
	finalPath := filepath.Join(blobsDir, digest)
	err = os.Rename(uploadPath, finalPath)
	if err != nil {
		h.log.Error("Error moving upload file", "uploadPath", uploadPath, "finalPath", finalPath, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set headers for the response
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", name, digest))
	w.WriteHeader(http.StatusCreated)
}

func Listen(log *slog.Logger, ec *entityserver.Client, path, addr string) error {
	// Create storage directory if it doesn't exist
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	// Create registry handler
	registry := NewRegistryHandler(path, log, ec)

	mux := http.NewServeMux()

	// Set up HTTP server
	mux.Handle("/v2/", registry)

	// Add basic health check endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "OK",
			"message": "OCI Registry is running",
		})
	})

	port := 5000
	log.Info("Starting OCI Registry", "port", port, "path", path)

	return http.ListenAndServe(addr, mux)
}
