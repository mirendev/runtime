cache_path = "./data/cache"

storage {
  s3 {
    bucket = "lsvdinttest"
    region = "us-east-1"
    access_key = "admin"
    secret_key = "password"
    host = "http://localhost:9000"
  }
}
