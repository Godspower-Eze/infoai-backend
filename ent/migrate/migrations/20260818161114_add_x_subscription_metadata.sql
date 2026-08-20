-- Modify "x_accounts" table
ALTER TABLE "x_accounts" ADD COLUMN "subscription_type" character varying NOT NULL DEFAULT '', ADD COLUMN "subscription_checked_at" timestamptz NULL;
