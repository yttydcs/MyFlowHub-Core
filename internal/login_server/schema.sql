-- Postgres schema for login server
CREATE SEQUENCE IF NOT EXISTS node_seq START WITH 2 INCREMENT BY 1 MINVALUE 1;

CREATE TABLE IF NOT EXISTS devices (
    device_id  TEXT PRIMARY KEY,
    credential TEXT NOT NULL,
    node_id    INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_devices_node_id ON devices(node_id);
