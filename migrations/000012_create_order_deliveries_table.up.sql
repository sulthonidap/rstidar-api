-- Proof of delivery (POD): filled by the courier in the app when finishing an
-- order — photos, who received it, and where/when. One per order.
CREATE TABLE order_deliveries (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT UNSIGNED NOT NULL,
    receiver_name VARCHAR(255) NOT NULL,
    receiver_relation VARCHAR(30) NOT NULL,
    note TEXT NULL,
    photos TEXT NOT NULL, -- JSON array of storage keys
    lat DECIMAL(10,7) NULL,
    lng DECIMAL(10,7) NULL,
    accuracy_m INT NULL,
    delivered_by BIGINT UNSIGNED NULL,
    delivered_at DATETIME NOT NULL,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,

    UNIQUE KEY order_deliveries_order_id_unique (order_id),
    CONSTRAINT order_deliveries_order_id_foreign FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE CASCADE,
    CONSTRAINT order_deliveries_delivered_by_foreign FOREIGN KEY (delivered_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
