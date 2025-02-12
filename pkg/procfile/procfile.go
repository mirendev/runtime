package procfile

import (
	"bytes"
	"strings"
)

func Parser(buf []byte) (map[string]string, error) {
	lines := bytes.Split(buf, []byte{'\n'})
	services := make(map[string]string)

	for _, line := range lines {
		line = bytes.TrimSpace(line)

		if len(line) == 0 || line[0] == '#' {
			continue
		}

		service, command, ok := strings.Cut(string(line), ":")
		if ok && service != "" && command != "" {
			services[service] = strings.TrimSpace(command)
		}
	}

	return services, nil
}
