CREATE TABLE couriers (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    organization_id BIGINT UNSIGNED NULL,
    vehicle_type VARCHAR(255) NULL,
    phone VARCHAR(255) NULL,
    is_active TINYINT(1) NOT NULL DEFAULT 1,
    last_lat DECIMAL(10,7) NULL,
    last_lng DECIMAL(10,7) NULL,
    last_location_at DATETIME NULL,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,
    CONSTRAINT couriers_user_id_foreign FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT couriers_organization_id_foreign FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
