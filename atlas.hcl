env "local" {
  url = getenv("DATABASE_URL")

  migration {
    dir = "file://ent/migrate/migrations"
  }
}
