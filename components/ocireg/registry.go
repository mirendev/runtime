package ocireg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/workloadidentity"
)

const (
	Host     = "cluster.local:5000"
	Audience = "miren-registry"
)

// RegistryHandler processes all OCI registry requests
type Registry struct {
	RootDir string
	Log     *slog.Logger
	EC      *entityserver.Client
	Issuer  *workloadidentity.Issuer

	server *http.Server
}

// NewRegistry creates a new Registry.
func NewRegistry(rootDir string, log *slog.Logger, ec *entityserver.Client, issuer *workloadidentity.Issuer) *Registry {
	return &Registry{
		RootDir: rootDir,
		Log:     log,
		EC:      ec,
		Issuer:  issuer,
	}
}

func (r *Registry) Start(ctx context.Context, addr string) error {
	path := filepath.Join(r.RootDir, "registry")

	// Create storage directory if it doesn't exist
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return err
	}

	r.Log.Info("Starting OCI Registry", "addr", addr, "path", path)

	r.server = &http.Server{
		Addr:    addr,
		Handler: newMux(NewRegistryHandler(path, r.Log, r.EC), r.Issuer),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	go func() {
		if err := r.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.Log.Error("OCI registry stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.server.Shutdown(shutdownCtx); err != nil {
			r.Log.Warn("OCI registry shutdown", "error", err)
		}
	}()
	return nil
}

// newMux wires the registry handler up alongside the health check endpoint.
func newMux(registry *RegistryHandler, issuer *workloadidentity.Issuer) *http.ServeMux {
	mux := http.NewServeMux()

	mux.Handle("/v2/", authorizeRegistry(registry, issuer, registry.log))

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

	return mux
}

func authorizeRegistry(next http.Handler, issuer *workloadidentity.Issuer, log *slog.Logger) http.Handler {
	if issuer == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			registryUnauthorized(w, r)
			return
		}

		claims, err := issuer.VerifyToken(token, Audience)
		if err != nil {
			log.Warn("rejected unauthenticated registry request", "error", err, "remote", r.RemoteAddr)
			registryUnauthorized(w, r)
			return
		}

		switch claims.SystemWorkload {
		case workloadidentity.SystemWorkloadBuildKit:
			if claims.IdentityType == workloadidentity.IdentityTypeSystem {
				next.ServeHTTP(w, r)
				return
			}
		case workloadidentity.SystemWorkloadSandboxController:
			if claims.IdentityType == workloadidentity.IdentityTypeSystem &&
				(r.Method == http.MethodGet || r.Method == http.MethodHead) {
				next.ServeHTTP(w, r)
				return
			}
		case workloadidentity.SystemWorkloadTelemetryWriter:
		}

		log.Warn("rejected unauthorized registry request",
			"identity_type", claims.IdentityType,
			"system_workload", claims.SystemWorkload,
			"method", r.Method,
			"remote", r.RemoteAddr)
		w.WriteHeader(http.StatusForbidden)
	})
}

func registryUnauthorized(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="%s://%s/v2/token",service="%s"`, scheme, r.Host, Audience))
	w.WriteHeader(http.StatusUnauthorized)
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

// checkName rejoins and validates the repository name segments, answering 400
// and reporting false if they don't form a legal name.
func (h *RegistryHandler) checkName(w http.ResponseWriter, r *http.Request, segments []string) (string, bool) {
	name := strings.Join(segments, "/")
	if err := validateRepositoryName(name); err != nil {
		h.reject(w, r, err)
		return "", false
	}
	return name, true
}

// reject logs a refused request and answers 400. The remote address is
// included because a rejection here means something on the bridge sent us a
// path it had no business sending.
func (h *RegistryHandler) reject(w http.ResponseWriter, r *http.Request, err error) {
	h.log.Warn("rejected registry request", "error", err, "remote", r.RemoteAddr, "path", r.URL.EscapedPath())
	w.WriteHeader(http.StatusBadRequest)
}

// blobPath resolves the storage location of a blob. The digest must already
// have passed validateDigest.
func (h *RegistryHandler) blobPath(digest string) (string, error) {
	return containedPath(h.storageRoot, "blobs", digest)
}

// uploadPath resolves the staging location of an in-flight upload. The id must
// already have passed validateUploadID.
func (h *RegistryHandler) uploadPath(id string) (string, error) {
	return containedPath(h.storageRoot, "uploads", id)
}

// ServeHTTP implements the http.Handler interface.
//
// Every path component is validated against the OCI grammars here, before
// dispatch, so a handler never sees a name, digest, or upload id that could
// reach outside the storage root.
func (h *RegistryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts, ok := splitEscapedPath(r.URL.EscapedPath())
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Basic API version check endpoint
	if len(parts) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	// The upload cases come first: "/v2/<name>/blobs/uploads" also satisfies
	// the plain blob pattern, with "uploads" standing in for the digest.
	switch {
	// /v2/<name>/blobs/uploads/<uuid>
	case len(parts) >= 4 && parts[len(parts)-3] == "blobs" && parts[len(parts)-2] == "uploads":
		name, ok := h.checkName(w, r, parts[:len(parts)-3])
		if !ok {
			return
		}
		uuid := parts[len(parts)-1]

		// A trailing slash leaves an empty final segment, which is the other
		// spelling of the upload-initiation route.
		if uuid == "" {
			if r.Method == http.MethodPost {
				h.initBlobUpload(w, name)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}

		if err := validateUploadID(uuid); err != nil {
			h.reject(w, r, err)
			return
		}

		switch r.Method {
		case http.MethodPut:
			h.completeBlobUpload(w, r, name, uuid)
		case http.MethodPatch:
			h.chunkBlobUpload(w, r, name, uuid)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/blobs/uploads
	case len(parts) >= 3 && parts[len(parts)-2] == "blobs" && parts[len(parts)-1] == "uploads":
		name, ok := h.checkName(w, r, parts[:len(parts)-2])
		if !ok {
			return
		}

		if r.Method == http.MethodPost {
			h.initBlobUpload(w, name)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/blobs/<digest>
	case len(parts) >= 3 && parts[len(parts)-2] == "blobs":
		if _, ok := h.checkName(w, r, parts[:len(parts)-2]); !ok {
			return
		}
		digest := parts[len(parts)-1]

		if err := validateDigest(digest); err != nil {
			h.reject(w, r, err)
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.getBlob(w, digest)
		case http.MethodHead:
			h.headBlob(w, digest)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	// /v2/<name>/manifests/<reference>
	case len(parts) >= 3 && parts[len(parts)-2] == "manifests":
		name, ok := h.checkName(w, r, parts[:len(parts)-2])
		if !ok {
			return
		}
		reference := parts[len(parts)-1]

		if err := validateReference(reference); err != nil {
			h.reject(w, r, err)
			return
		}

		switch r.Method {
		case http.MethodGet:
			h.getManifest(w, r, reference)
		case http.MethodPut:
			h.putManifest(w, r, name, reference)
		case http.MethodHead:
			h.headManifest(w, r, reference)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// getManifest handles GET requests for manifests
func (h *RegistryHandler) getManifest(w http.ResponseWriter, r *http.Request, reference string) {
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
func (h *RegistryHandler) headManifest(w http.ResponseWriter, r *http.Request, reference string) {
	var appVer core_v1alpha.Artifact

	err := h.ec.Get(r.Context(), reference, &appVer)
	if err != nil {
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

	// Check if an artifact with this manifest digest already exists
	var existingArtifact core_v1alpha.Artifact
	err = h.ec.OneAtIndex(r.Context(), entity.String(core_v1alpha.ArtifactManifestDigestId, digest), &existingArtifact)
	if err == nil {
		// Artifact already exists, return success without creating a duplicate
		h.log.Info("Found existing artifact with same digest", "digest", digest, "existing_id", existingArtifact.ID, "reference", reference)
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusCreated)
		return
	}

	// Only proceed to create a new artifact if one doesn't exist
	var (
		app      core_v1alpha.App
		artifact core_v1alpha.Artifact
	)

	artifact.Manifest = string(manifestData)
	artifact.ManifestDigest = digest
	artifact.Status = core_v1alpha.ACTIVE

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
func (h *RegistryHandler) getBlob(w http.ResponseWriter, digest string) {
	blobPath, err := h.blobPath(digest)
	if err != nil {
		h.log.Error("Refusing blob read outside storage root", "digest", digest, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

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
func (h *RegistryHandler) headBlob(w http.ResponseWriter, digest string) {
	blobPath, err := h.blobPath(digest)
	if err != nil {
		h.log.Error("Refusing blob stat outside storage root", "digest", digest, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	fileInfo, err := os.Stat(blobPath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	w.WriteHeader(http.StatusOK)
}

// initBlobUpload handles POST requests to initialize blob uploads
func (h *RegistryHandler) initBlobUpload(w http.ResponseWriter, name string) {
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
	uploadPath, err := h.uploadPath(uploadID)
	if err != nil {
		h.log.Error("Error resolving upload path", "uploadID", uploadID, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

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
	uploadPath, err := h.uploadPath(uuid)
	if err != nil {
		h.log.Error("Refusing upload write outside storage root", "uuid", uuid, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Check if the upload exists
	_, err = os.Stat(uploadPath)
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
	uploadPath, err := h.uploadPath(uuid)
	if err != nil {
		h.log.Error("Refusing upload completion outside storage root", "uuid", uuid, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Check if the upload exists
	_, err = os.Stat(uploadPath)
	if err != nil {
		h.log.Error("Error stating upload file", "uploadPath", uploadPath, "error", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// The digest arrives on the query string, so unlike the path components it
	// gets no protection at all from the routing. It is the sink from
	// MIR-1474: before this check, ?digest=../../../etc/cron.d/pwn made the
	// rename below an arbitrary file write as root.
	digest := r.URL.Query().Get("digest")
	if err := validateDigest(digest); err != nil {
		h.reject(w, r, err)
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

	finalPath, err := h.blobPath(digest)
	if err != nil {
		h.log.Error("Refusing blob write outside storage root", "digest", digest, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Append any final data from the request body. This copies unconditionally
	// rather than gating on ContentLength, which is -1 for a chunked request:
	// under the digest check below, skipping a chunked body would turn a
	// silently truncated blob into a hard push failure. io.Copy on an empty
	// body is a no-op, so the monolithic and chunked cases both work.
	if r.Body != nil {
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

	// Confirm the content actually hashes to the digest the client claimed.
	// Without this any sandbox can push whatever it likes under an existing
	// blob's digest, and the rename below silently clobbers the original.
	actual, err := digestFile(uploadPath, digest)
	if err != nil {
		os.Remove(uploadPath)
		h.log.Warn("Rejecting blob upload", "uploadPath", uploadPath, "digest", digest, "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if actual != digest {
		os.Remove(uploadPath)
		h.log.Warn("Rejecting blob upload with mismatched digest",
			"claimed", digest, "actual", actual, "remote", r.RemoteAddr)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Move the upload to the final location
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
