-- Payment receipts are no longer uploaded: for non-cash orders the CCO checks
-- the transfer/QRIS coming in and marks the order paid. Transactions still
-- "waiting for verification" go back to unpaid for the CCO to check. Old
-- receipts (proof, proof_uploaded_at) are kept as history.
UPDATE transactions SET payment_status = 'unpaid' WHERE payment_status = 'verifying';
