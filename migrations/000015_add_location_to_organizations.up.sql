-- Exact map point of the RS (pickup): set by the admin on a map, used as the
-- pickup point of its orders and for courier navigation.
ALTER TABLE organizations
    ADD COLUMN lat DECIMAL(10,7) NULL AFTER address,
    ADD COLUMN lng DECIMAL(10,7) NULL AFTER lat;
