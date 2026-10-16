-- A courier's delivery route: the order they plan to drop their packages in,
-- saved from "Urutan antar" in the courier app so Ops/Admin can follow it on
-- the panel map. One open route per courier (finished_at NULL); re-planning
-- updates it, and it's closed once every package on it is delivered/cancelled.
CREATE TABLE courier_routes (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    courier_id BIGINT UNSIGNED NOT NULL,
    start_lat DECIMAL(10,7) NULL, -- where the courier was when they set off (NULL = from the RS)
    start_lng DECIMAL(10,7) NULL,
    started_at DATETIME NOT NULL,
    finished_at DATETIME NULL,
    created_at DATETIME NULL,
    updated_at DATETIME NULL,

    KEY courier_routes_courier_open_index (courier_id, finished_at),
    KEY courier_routes_started_at_index (started_at),
    CONSTRAINT courier_routes_courier_id_foreign FOREIGN KEY (courier_id) REFERENCES couriers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- The stops in delivery order (seq 1, 2, …), one per package.
CREATE TABLE courier_route_stops (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    route_id BIGINT UNSIGNED NOT NULL,
    order_id BIGINT UNSIGNED NOT NULL,
    seq INT UNSIGNED NOT NULL,

    UNIQUE KEY courier_route_stops_route_order_unique (route_id, order_id),
    CONSTRAINT courier_route_stops_route_id_foreign FOREIGN KEY (route_id) REFERENCES courier_routes (id) ON DELETE CASCADE,
    CONSTRAINT courier_route_stops_order_id_foreign FOREIGN KEY (order_id) REFERENCES orders (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
