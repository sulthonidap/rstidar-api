-- New pickup flow: farmasi marks "siap diambil", CCO collects & packs, the
-- courier scans the label to take the package. Assigning a courier is no
-- longer a status, so orders still "assigned" continue as "packed".
ALTER TABLE orders
    ADD COLUMN ready_at DATETIME NULL AFTER confirmed_at,
    ADD COLUMN collected_at DATETIME NULL AFTER ready_at,
    ADD COLUMN packed_at DATETIME NULL AFTER collected_at;

UPDATE orders SET status = 'packed', packed_at = COALESCE(assigned_at, updated_at) WHERE status = 'assigned';

INSERT INTO roles (name, label, created_at, updated_at)
SELECT 'farmasi', 'Farmasi RS', NOW(), NOW() FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'farmasi');
