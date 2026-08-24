-- Modify "post_items" table
ALTER TABLE "post_items" ADD COLUMN "submission_state" character varying NOT NULL DEFAULT 'not_started', ADD COLUMN "submission_started_at" timestamptz NULL, ADD COLUMN "outcome_confirmed_at" timestamptz NULL, ADD COLUMN "outcome_confirmed_by" uuid NULL, ADD COLUMN "confirmed_x_post_url" character varying NULL;
