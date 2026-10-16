-- Orders that had a receipt and are still unpaid were "waiting for verification".
UPDATE transactions SET payment_status = 'verifying'
WHERE payment_status = 'unpaid' AND proof IS NOT NULL AND proof <> '';
