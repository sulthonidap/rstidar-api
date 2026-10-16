CREATE TABLE order_status_history (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT UNSIGNED NOT NULL,
    from_status VARCHAR(20) NULL,
    to_status VARCHAR(20) NOT NULL,
    changed_by BIGINT UNSIGNED NULL,
    note TEXT NULL,
    changed_at DATETIME NOT NULL,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,
    KEY order_status_history_order_id_changed_at_index (order_id, changed_at),
    CONSTRAINT osh_order_id_foreign FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE CASCADE,
    CONSTRAINT osh_changed_by_foreign FOREIGN KEY (changed_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
