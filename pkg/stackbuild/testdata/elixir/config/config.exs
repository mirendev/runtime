import Config

config :hello, greeting: System.get_env("GREETING", "hi")
