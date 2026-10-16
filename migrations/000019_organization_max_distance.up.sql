-- max_distance_km: the RS's service area — the farthest an order may go from
-- it (RS → destination, by road); NULL = no limit. Orders beyond it are refused.
ALTER TABLE organizations ADD COLUMN max_distance_km DECIMAL(6,2) NULL AFTER lng;
