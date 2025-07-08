require 'sinatra'

set :port, ENV['PORT'] || 3000
set :bind, '0.0.0.0'

get '/' do
  "Hello from Ruby!\n"
end

get '/health' do
  "OK\n"
end