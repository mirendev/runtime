FROM ubuntu:22.04

RUN apt update && apt install -y iproute2 python3

CMD ["sh", "-c", "exec python3 -m http.server $PORT"]
