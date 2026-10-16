-- Square thumbnails of the POD photos (JSON array of storage keys, same order
-- as photos). NULL for photos uploaded before thumbnails existed.
ALTER TABLE order_deliveries ADD COLUMN thumbs TEXT NULL AFTER photos;
