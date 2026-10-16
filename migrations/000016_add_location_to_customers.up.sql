-- Saved map point of a customer's address, reused for their next orders.
ALTER TABLE customers
    ADD COLUMN lat DECIMAL(10,7) NULL AFTER address,
    ADD COLUMN lng DECIMAL(10,7) NULL AFTER lat;
