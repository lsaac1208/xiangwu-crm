ALTER TABLE leads ADD COLUMN customer_mobile_hash varchar(96);
CREATE INDEX idx_leads_customer_mobile_hash ON leads(customer_mobile_hash);
