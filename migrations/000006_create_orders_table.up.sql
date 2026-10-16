CREATE TABLE orders (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(255) NOT NULL,
    organization_id BIGINT UNSIGNED NULL,

    customer_name VARCHAR(255) NOT NULL,
    customer_phone VARCHAR(255) NOT NULL,
    pickup_address TEXT NOT NULL,
    dropoff_address TEXT NOT NULL,
    pickup_lat DECIMAL(10,7) NULL,
    pickup_lng DECIMAL(10,7) NULL,
    dropoff_lat DECIMAL(10,7) NULL,
    dropoff_lng DECIMAL(10,7) NULL,

    service_id BIGINT UNSIGNED NULL,
    courier_id BIGINT UNSIGNED NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    source VARCHAR(20) NOT NULL DEFAULT 'public',
    created_by BIGINT UNSIGNED NULL,

    price DECIMAL(12,2) NOT NULL DEFAULT 0,
    notes TEXT NULL,

    confirmed_at DATETIME NULL,
    assigned_at DATETIME NULL,
    picked_up_at DATETIME NULL,
    delivered_at DATETIME NULL,
    cancelled_at DATETIME NULL,

    created_at DATETIME NULL,
    updated_at DATETIME NULL,

    UNIQUE KEY orders_code_unique (code),
    KEY orders_status_index (status),
    CONSTRAINT orders_organization_id_foreign FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE SET NULL,
    CONSTRAINT orders_service_id_foreign FOREIGN KEY (service_id) REFERENCES services (id) ON DELETE SET NULL,
    CONSTRAINT orders_courier_id_foreign FOREIGN KEY (courier_id) REFERENCES couriers (id) ON DELETE SET NULL,
    CONSTRAINT orders_created_by_foreign FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
