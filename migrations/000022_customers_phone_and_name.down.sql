-- Fails if one phone now has several customers (names); merge those first.
-- Same FK dance as the up migration: keep an index on organization_id meanwhile.
ALTER TABLE customers ADD INDEX customers_organization_id_index (organization_id);
ALTER TABLE customers DROP INDEX customers_organization_phone_name_unique;
ALTER TABLE customers ADD UNIQUE KEY customers_organization_phone_unique (organization_id, phone);
ALTER TABLE customers DROP INDEX customers_organization_id_index;
