#!/bin/sh
echo "entrypoint-args-image: pid=$$ argc=$#"
i=0
for a in "$@"; do
	echo "entrypoint-args-image: arg[$i]=<$a>"
	i=$((i + 1))
done
exec python3 -m http.server "${PORT:-3000}" --directory /www
