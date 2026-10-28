-- Program ongkir gratis (mis. lansia & disabilitas berdomisili Kota Magelang).
-- Petugas yang memverifikasi: memilih kategori pasien dan mencentang bahwa
-- domisilinya sudah dicek (KTP). Kalau gratis, price = 0 dan tarif normalnya
-- disimpan di normal_price untuk laporan subsidi.
ALTER TABLE orders
    ADD COLUMN patient_category VARCHAR(20) NOT NULL DEFAULT 'umum' AFTER medical_record_no,
    ADD COLUMN domicile_checked TINYINT(1) NOT NULL DEFAULT 0 AFTER patient_category,
    ADD COLUMN is_free TINYINT(1) NOT NULL DEFAULT 0 AFTER domicile_checked,
    ADD COLUMN normal_price DECIMAL(12,2) NULL AFTER is_free,
    ADD COLUMN free_verified_by BIGINT UNSIGNED NULL AFTER normal_price,
    ADD COLUMN free_verified_at DATETIME NULL AFTER free_verified_by,
    ADD KEY orders_is_free_index (is_free),
    ADD CONSTRAINT orders_free_verified_by_foreign FOREIGN KEY (free_verified_by) REFERENCES users (id) ON DELETE SET NULL;
