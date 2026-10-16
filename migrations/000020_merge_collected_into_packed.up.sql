-- "Diambil dari RS" (collected) and "Siap kirim" (packed) are now one CCO step:
-- the CCO collects the medicine and packs it, then presses one button. Orders
-- still sitting in "collected" continue as "packed". collected_at is kept and
-- still filled (together with packed_at) for reports. Old status history rows
-- keep "collected" so the timeline shows what really happened.
UPDATE orders
SET status = 'packed', packed_at = COALESCE(packed_at, collected_at, updated_at)
WHERE status = 'collected';
