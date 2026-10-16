UPDATE orders SET status = 'assigned' WHERE status IN ('ready_for_pickup', 'collected', 'packed') AND courier_id IS NOT NULL;
UPDATE orders SET status = 'confirmed' WHERE status IN ('ready_for_pickup', 'collected', 'packed');

ALTER TABLE orders
    DROP COLUMN packed_at,
    DROP COLUMN collected_at,
    DROP COLUMN ready_at;
