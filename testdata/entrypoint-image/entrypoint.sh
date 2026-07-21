#!/bin/sh
# Fixture for MIR-1444: log argv verbatim so the blackbox test can assert the
# service launched in exec form. When run as the image's own ENTRYPOINT+CMD
# (no /bin/sh -c), the $-literal CMD arg arrives unexpanded and this is PID 1.
echo "entrypoint-image: pid=$$ argc=$#"
i=0
for a in "$@"; do
	echo "entrypoint-image: arg[$i]=<$a>"
	i=$((i + 1))
done
exec python3 -m http.server "${PORT:-3000}" --directory /www
