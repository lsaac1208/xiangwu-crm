-- Existing conversions keep unknown payment status until reviewed by their owner.
ALTER TABLE leads ADD COLUMN payment_status varchar(16) NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN refund_amount decimal(12,2) NOT NULL DEFAULT 0;
