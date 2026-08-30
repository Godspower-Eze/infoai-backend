-- Create "storage_deletions" table
CREATE TABLE "storage_deletions" ("id" uuid NOT NULL, "storage_key" character varying NOT NULL, "attempt_count" bigint NOT NULL DEFAULT 0, "next_attempt_at" timestamptz NULL, "last_error_code" character varying NULL, "last_error_message" character varying NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "storage_deletions_storage_key_key" to table: "storage_deletions"
CREATE UNIQUE INDEX "storage_deletions_storage_key_key" ON "storage_deletions" ("storage_key");
-- Modify "x_accounts" table
ALTER TABLE "x_accounts" ADD COLUMN "rate_limit_remaining" bigint NULL, ADD COLUMN "rate_limit_reset_at" timestamptz NULL;
-- Create "posts" table
CREATE TABLE "posts" ("id" uuid NOT NULL, "creation_mode" character varying NOT NULL, "status" character varying NOT NULL DEFAULT 'draft', "scheduled_at" timestamptz NULL, "publish_requested_at" timestamptz NULL, "published_at" timestamptz NULL, "next_attempt_at" timestamptz NULL, "active_river_job_id" bigint NULL, "attempt_count" bigint NOT NULL DEFAULT 0, "last_error_code" character varying NULL, "last_error_message" character varying NULL, "lease_token" uuid NULL, "lease_owner" character varying NULL, "lease_expires_at" timestamptz NULL, "lease_version" bigint NOT NULL DEFAULT 0, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "owner_id" uuid NOT NULL, "x_account_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "posts_users_posts" FOREIGN KEY ("owner_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT "posts_x_accounts_posts" FOREIGN KEY ("x_account_id") REFERENCES "x_accounts" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "post_owner_id_created_at_id" to table: "posts"
CREATE INDEX "post_owner_id_created_at_id" ON "posts" ("owner_id", "created_at", "id");
-- Create index "post_status_lease_expires_at" to table: "posts"
CREATE INDEX "post_status_lease_expires_at" ON "posts" ("status", "lease_expires_at");
-- Create index "post_status_next_attempt_at" to table: "posts"
CREATE INDEX "post_status_next_attempt_at" ON "posts" ("status", "next_attempt_at");
-- Create index "post_status_scheduled_at" to table: "posts"
CREATE INDEX "post_status_scheduled_at" ON "posts" ("status", "scheduled_at");
-- Create "post_items" table
CREATE TABLE "post_items" ("id" uuid NOT NULL, "position" bigint NOT NULL, "text" character varying NOT NULL DEFAULT '', "x_post_id" character varying NULL, "published_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "post_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "post_items_posts_items" FOREIGN KEY ("post_id") REFERENCES "posts" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "post_items_x_post_id_key" to table: "post_items"
CREATE UNIQUE INDEX "post_items_x_post_id_key" ON "post_items" ("x_post_id");
-- Create index "postitem_post_id_position" to table: "post_items"
CREATE UNIQUE INDEX "postitem_post_id_position" ON "post_items" ("post_id", "position");
-- Create "media_assets" table
CREATE TABLE "media_assets" ("id" uuid NOT NULL, "position" bigint NOT NULL, "storage_key" character varying NOT NULL, "original_filename" character varying NOT NULL, "mime_type" character varying NOT NULL, "size_bytes" bigint NOT NULL, "sha256_checksum" bytea NOT NULL, "alt_text" character varying NULL, "created_at" timestamptz NOT NULL, "post_item_id" uuid NOT NULL, "owner_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "media_assets_post_items_media_assets" FOREIGN KEY ("post_item_id") REFERENCES "post_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, CONSTRAINT "media_assets_users_media_assets" FOREIGN KEY ("owner_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "media_assets_storage_key_key" to table: "media_assets"
CREATE UNIQUE INDEX "media_assets_storage_key_key" ON "media_assets" ("storage_key");
-- Create index "mediaasset_post_item_id_position" to table: "media_assets"
CREATE UNIQUE INDEX "mediaasset_post_item_id_position" ON "media_assets" ("post_item_id", "position");
-- Create "publication_attempts" table
CREATE TABLE "publication_attempts" ("id" uuid NOT NULL, "attempt_number" bigint NOT NULL, "trigger" character varying NOT NULL, "outcome" character varying NOT NULL DEFAULT 'pending', "retryable" boolean NOT NULL DEFAULT false, "error_code" character varying NULL, "error_message" character varying NULL, "started_at" timestamptz NOT NULL, "finished_at" timestamptz NULL, "post_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "publication_attempts_posts_attempts" FOREIGN KEY ("post_id") REFERENCES "posts" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "publicationattempt_post_id_attempt_number" to table: "publication_attempts"
CREATE UNIQUE INDEX "publicationattempt_post_id_attempt_number" ON "publication_attempts" ("post_id", "attempt_number");
