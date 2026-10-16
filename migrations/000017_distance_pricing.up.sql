-- Distance-based pricing: each service can have tiers ("up to X km → price")
-- plus a per-km charge beyond the last tier; orders remember the billed distance.
CREATE TABLE service_tiers (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    service_id BIGINT UNSIGNED NOT NULL,
    up_to_km DECIMAL(6,2) NOT NULL,
    price DECIMAL(12,2) NOT NULL,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,
    UNIQUE KEY service_tiers_service_km_unique (service_id, up_to_km),
    CONSTRAINT service_tiers_service_id_foreign FOREIGN KEY (service_id) REFERENCES services (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE services ADD COLUMN price_per_km DECIMAL(12,2) NULL AFTER price_base;
-- distance_km: road distance RS → destination; distance_source: 'road' (routing
-- server) or 'estimate' (straight line × 1.3 when the routing server was unreachable).
ALTER TABLE orders
    ADD COLUMN distance_km DECIMAL(8,2) NULL AFTER price,
    ADD COLUMN distance_source VARCHAR(10) NULL AFTER distance_km;
