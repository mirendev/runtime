#!/bin/sh
while true; do
  printf "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok" | nc -l -p 3000
done
