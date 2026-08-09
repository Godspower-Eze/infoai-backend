-- Create "sessions" table
CREATE TABLE "sessions" ("token" character varying NOT NULL, "data" bytea NOT NULL, "expiry" timestamptz NOT NULL, PRIMARY KEY ("token"));
-- Create index "scssession_expiry" to table: "sessions"
CREATE INDEX "scssession_expiry" ON "sessions" ("expiry");
-- Create "users" table
CREATE TABLE "users" ("id" uuid NOT NULL, "email" character varying NOT NULL, "password_hash" character varying NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");
-- Create "x_accounts" table
CREATE TABLE "x_accounts" ("id" uuid NOT NULL, "x_user_id" character varying NOT NULL, "username" character varying NOT NULL, "display_name" character varying NOT NULL DEFAULT '', "profile_image_url" character varying NULL, "access_token" bytea NOT NULL, "refresh_token" bytea NULL, "token_expiry" timestamptz NOT NULL, "scopes" jsonb NOT NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "user_x_accounts" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "x_accounts_users_x_accounts" FOREIGN KEY ("user_x_accounts") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "x_accounts_x_user_id_key" to table: "x_accounts"
CREATE UNIQUE INDEX "x_accounts_x_user_id_key" ON "x_accounts" ("x_user_id");
