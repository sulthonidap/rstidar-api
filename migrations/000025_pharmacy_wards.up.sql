-- Farmasi pada order: rawat jalan (+ no. antrian farmasi) atau rawat inap
-- (+ bangsal). Bangsal adalah master data yang diatur Admin.
CREATE TABLE wards (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,
    UNIQUE KEY wards_name_unique (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Daftar bangsal awal dari RS (bisa diubah di Pengaturan → Bangsal).
INSERT INTO wards (name, created_at, updated_at) VALUES
    ('Alamanda 1', NOW(), NOW()), ('Alamanda 2', NOW(), NOW()), ('Anyelir', NOW(), NOW()),
    ('Aster 2', NOW(), NOW()), ('Aster 3', NOW(), NOW()), ('Aster 4', NOW(), NOW()),
    ('Aster 5', NOW(), NOW()), ('Aster 6', NOW(), NOW()), ('Bougenville', NOW(), NOW()),
    ('Cempaka', NOW(), NOW()), ('Dahlia 1', NOW(), NOW()), ('Dahlia 2', NOW(), NOW()),
    ('Dahlia 3', NOW(), NOW()), ('Dahlia 4', NOW(), NOW()), ('Dahlia 5', NOW(), NOW()),
    ('Edelweis 2', NOW(), NOW()), ('Edelweis 3', NOW(), NOW()), ('Edelweis 4', NOW(), NOW()),
    ('Edelweis 5', NOW(), NOW()), ('Edelweis 6', NOW(), NOW()), ('ICU', NOW(), NOW()),
    ('PICU', NOW(), NOW());

ALTER TABLE orders
    ADD COLUMN care_type VARCHAR(20) NULL AFTER free_verified_at,
    ADD COLUMN pharmacy_queue_no VARCHAR(20) NULL AFTER care_type,
    ADD COLUMN ward_id BIGINT UNSIGNED NULL AFTER pharmacy_queue_no,
    ADD KEY orders_care_type_index (care_type),
    ADD CONSTRAINT orders_ward_id_foreign FOREIGN KEY (ward_id) REFERENCES wards (id) ON DELETE SET NULL;
