ALTER TABLE orders DROP COLUMN distance_source, DROP COLUMN distance_km;
ALTER TABLE services DROP COLUMN price_per_km;
DROP TABLE IF EXISTS service_tiers;
