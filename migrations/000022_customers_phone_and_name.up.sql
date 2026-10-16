-- A customer is now one person: the same phone AND name. A phone can be shared
-- (a child ordering for both parents), and the old phone-only key made each new
-- name overwrite the previous customer.

-- The old unique key also serves the organization_id foreign key, which won't
-- let it go: give the FK its own index first.
ALTER TABLE customers ADD INDEX customers_organization_id_index (organization_id);
ALTER TABLE customers DROP INDEX customers_organization_phone_unique;

-- Phones are stored the same way however they were typed: digits only, 62… → 0….
UPDATE customers SET phone = REGEXP_REPLACE(phone, '[^0-9]', '');
UPDATE customers SET phone = CONCAT('0', SUBSTRING(phone, 3)) WHERE phone LIKE '62%';

-- Normalizing can make two rows the same person: keep the oldest, move the
-- orders over, drop the copy.
UPDATE orders o
JOIN customers dup ON dup.id = o.customer_id
JOIN customers keep ON keep.organization_id = dup.organization_id AND keep.phone = dup.phone
    AND keep.name = dup.name AND keep.id < dup.id
SET o.customer_id = keep.id;
DELETE dup FROM customers dup
JOIN customers keep ON keep.organization_id = dup.organization_id AND keep.phone = dup.phone
    AND keep.name = dup.name AND keep.id < dup.id;

-- The new key starts with organization_id, so it serves the FK from here on.
ALTER TABLE customers ADD UNIQUE KEY customers_organization_phone_name_unique (organization_id, phone, name);
ALTER TABLE customers DROP INDEX customers_organization_id_index;
