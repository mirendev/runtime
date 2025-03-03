package disk

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pkg/errors"
	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/asm/autoreg"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/units"
)

type Manager struct {
	Log *slog.Logger
	DB  *pgxpool.Pool

	OrgId       uint64 `asm:"org_id"`
	Provisioner *Provisioner
	DataPath    string `asm:"data-path"`

	dataRoot   string
	accessRoot string
}

var _ = autoreg.Register[Manager]()

func (m *Manager) Populated() error {
	m.dataRoot = filepath.Join(m.DataPath, "data")
	m.accessRoot = filepath.Join(m.DataPath, "access")

	if err := os.MkdirAll(m.dataRoot, 0755); err != nil {
		return errors.Wrap(err, "create data root")
	}

	if err := os.MkdirAll(m.accessRoot, 0755); err != nil {
		return errors.Wrap(err, "create access root")
	}

	return nil
}

type CreateDiskParams struct {
	Name     string
	Capacity units.Data
}

func (m *Manager) CreateDisk(ctx context.Context, params CreateDiskParams) (string, error) {
	sa := &lsvd.LocalFileAccess{Dir: m.dataRoot, Log: m.Log}

	_, err := sa.GetVolumeInfo(ctx, params.Name)
	if err == nil {
		return "", errors.New("disk already exists")
	}

	err = sa.InitVolume(ctx, &lsvd.VolumeInfo{
		Name: params.Name,
		Size: params.Capacity.Bytes(),
		UUID: uuid.NewString(),
	})
	if err != nil {
		return "", err
	}

	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", errors.Wrap(err, "begin transaction")
	}
	defer tx.Rollback(ctx)

	externalID := idgen.Gen("d")

	_, err = tx.Exec(ctx, `
		INSERT INTO disks (organization_id, name, external_id, capacity)
		VALUES ($1, $2, $3, $4)
	`, m.OrgId, params.Name, externalID, params.Capacity)
	if err != nil {
		return "", errors.Wrap(err, "insert disk")
	}

	// Create directories for the disk
	accessDir := filepath.Join(m.accessRoot, externalID)

	if err := os.MkdirAll(accessDir, 0755); err != nil {
		return "", errors.Wrap(err, "create access directory")
	}

	m.Log.Info("created disk", "name", params.Name, "external_id", externalID)
	// Provision the disk
	err = m.Provisioner.Provision(ctx, ProvisionConfig{
		Name:      params.Name,
		DataDir:   m.dataRoot,
		AccessDir: accessDir,
		LogFile:   filepath.Join(accessDir, "log"),
	})

	if err != nil {
		return "", errors.Wrap(err, "provision disk")
	}

	if err := tx.Commit(ctx); err != nil {
		return "", errors.Wrap(err, "commit transaction")
	}

	return filepath.Join(accessDir, "fs"), nil
}

func (m *Manager) GetDisk(ctx context.Context, name string) (*Disk, error) {
	var disk Disk
	err := m.DB.QueryRow(ctx, `
		SELECT id, created_at, updated_at, organization_id, name, external_id, capacity
		FROM disks
		WHERE organization_id = $1 AND name = $2
	`, m.OrgId, name).Scan(
		&disk.ID,
		&disk.CreatedAt,
		&disk.UpdatedAt,
		&disk.OrganizationID,
		&disk.Name,
		&disk.ExternalID,
		&disk.Capacity,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "query disk")
	}

	return &disk, nil
}

type Disk struct {
	ID             int64
	CreatedAt      string
	UpdatedAt      string
	OrganizationID int64
	Name           string
	ExternalID     string
	Capacity       int64
}

func (m *Manager) DeleteDisk(ctx context.Context, name string) error {
	tx, err := m.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return errors.Wrap(err, "begin transaction")
	}
	defer tx.Rollback(ctx)

	var externalID string
	err = tx.QueryRow(ctx, `
		DELETE FROM disks
		WHERE organization_id = $1 AND name = $2
		RETURNING external_id
	`, m.OrgId, name).Scan(&externalID)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "delete disk")
	}

	// Clean up directories
	dataDir := filepath.Join(m.dataRoot, externalID)
	accessDir := filepath.Join(m.accessRoot, externalID)

	os.RemoveAll(dataDir)
	os.RemoveAll(accessDir)

	if err := tx.Commit(ctx); err != nil {
		return errors.Wrap(err, "commit transaction")
	}

	return nil
}
