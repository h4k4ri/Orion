-- +migrate Up
CREATE SCHEMA IF NOT EXISTS orion_image;

CREATE TABLE IF NOT EXISTS orion_image.images (
    id          VARCHAR(255) PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    status      VARCHAR(50) NOT NULL,
    disk_format VARCHAR(50) NOT NULL,
    container_format VARCHAR(50) NOT NULL,
    visibility  VARCHAR(50) NOT NULL,
    architecture VARCHAR(50) NOT NULL,
    min_disk_gb INTEGER NOT NULL DEFAULT 0,
    size_bytes BIGINT NOT NULL,
    checksum_sha256 VARCHAR(64),
    path        TEXT
);

-- +migrate Down
DROP TABLE IF EXISTS orion_image.images;
DROP SCHEMA IF EXISTS orion_image;
