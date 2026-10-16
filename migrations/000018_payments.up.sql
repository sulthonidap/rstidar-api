-- Payments chosen when ordering: cash (paid at the RS cashier before the
-- medicine is collected) or non-cash (bank transfer / QRIS + proof, verified
-- by Finance before delivery). The transaction now exists from order creation.
ALTER TABLE transactions
    ADD COLUMN payment_method VARCHAR(10) NULL AFTER amount,
    ADD COLUMN proof VARCHAR(255) NULL AFTER payment_status,
    ADD COLUMN proof_uploaded_at DATETIME NULL AFTER proof,
    ADD COLUMN note VARCHAR(255) NULL AFTER proof_uploaded_at;

-- Small key/value settings (e.g. the bank account and QRIS for non-cash payments).
CREATE TABLE app_settings (
    name VARCHAR(64) NOT NULL PRIMARY KEY,
    value TEXT NULL,
    updated_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
