package login_server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Store defines persistence operations for device credentials.
type Store interface {
	UpsertDevice(ctx context.Context, deviceID string) (nodeID uint32, credential string, err error)
	GetDevice(ctx context.Context, deviceID string) (nodeID uint32, credential string, found bool, err error)
	DeleteDevice(ctx context.Context, deviceID string, credential string) (nodeID uint32, removed bool, mismatch bool, err error)
	Close() error
}

// PostgresStore implements Store on top of PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertDevice allocates a node_id and credential if the device does not exist; otherwise returns existing values.
func (s *PostgresStore) UpsertDevice(ctx context.Context, deviceID string) (uint32, string, error) {
	if deviceID == "" {
		return 0, "", errors.New("deviceID empty")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = rollbackIgnore(tx) }()

	var nodeID uint32
	var credential string
	row := tx.QueryRowContext(ctx, `SELECT node_id, credential FROM devices WHERE device_id=$1 FOR UPDATE`, deviceID)
	switch err := row.Scan(&nodeID, &credential); err {
	case nil:
		if err := tx.Commit(); err != nil {
			return 0, "", err
		}
		return nodeID, credential, nil
	case sql.ErrNoRows:
	default:
		return 0, "", err
	}

	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('node_seq')`).Scan(&seq); err != nil {
		return 0, "", err
	}
	nodeID = uint32(seq)
	credential = generateCredential()
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices (device_id, credential, node_id) VALUES ($1, $2, $3)`, deviceID, credential, nodeID); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return nodeID, credential, nil
}

func (s *PostgresStore) GetDevice(ctx context.Context, deviceID string) (uint32, string, bool, error) {
	if deviceID == "" {
		return 0, "", false, errors.New("deviceID empty")
	}
	var nodeID uint32
	var credential string
	err := s.db.QueryRowContext(ctx, `SELECT node_id, credential FROM devices WHERE device_id=$1`, deviceID).Scan(&nodeID, &credential)
	if err == sql.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return nodeID, credential, true, nil
}

// DeleteDevice removes a device if credential matches (when provided). Returns nodeID when found.
func (s *PostgresStore) DeleteDevice(ctx context.Context, deviceID string, credential string) (uint32, bool, bool, error) {
	if deviceID == "" {
		return 0, false, false, errors.New("deviceID empty")
	}
	var nodeID uint32
	var current string
	err := s.db.QueryRowContext(ctx, `SELECT node_id, credential FROM devices WHERE device_id=$1`, deviceID).Scan(&nodeID, &current)
	if err == sql.ErrNoRows {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	if credential != "" && credential != current {
		return nodeID, false, true, nil
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE device_id=$1`, deviceID)
	if err != nil {
		return nodeID, false, false, err
	}
	affected, _ := res.RowsAffected()
	return nodeID, affected > 0, false, nil
}

func rollbackIgnore(tx *sql.Tx) error {
	if tx == nil {
		return nil
	}
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}
