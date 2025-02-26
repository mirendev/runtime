package run

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/davecgh/go-spew/spew"
	_ "github.com/mattn/go-sqlite3"
)

type containerStore struct {
	db *sql.DB
}

func newContainerStore(path string) (*containerStore, error) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	spew.Dump(path)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS containers (
			id TEXT PRIMARY KEY,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create table: %w", err)
	}

	return &containerStore{db: db}, nil
}

func (s *containerStore) Save(cfg *ContainerConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO containers (id, config)
		VALUES (?, ?)
	`, cfg.Id, string(data))

	if err != nil {
		return fmt.Errorf("insert container: %w", err)
	}

	return nil
}

func (s *containerStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM containers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete container: %w", err)
	}
	return nil
}

func (s *containerStore) List() ([]*ContainerConfig, error) {
	rows, err := s.db.Query(`SELECT config FROM containers`)
	if err != nil {
		return nil, fmt.Errorf("query containers: %w", err)
	}
	defer rows.Close()

	var configs []*ContainerConfig
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan container: %w", err)
		}

		var cfg ContainerConfig
		if err := json.Unmarshal([]byte(data), &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		configs = append(configs, &cfg)
	}

	return configs, nil
}
