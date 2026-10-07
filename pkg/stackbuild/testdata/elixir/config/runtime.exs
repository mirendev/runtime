import Config

config :hello, api_token: System.fetch_env!("API_TOKEN")
