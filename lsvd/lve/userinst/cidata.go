package userinst

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"strings"

	"github.com/kdomanski/iso9660"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

func WriteCloudInit(ctx context.Context, log *slog.Logger, cfg *UserConfig, target string) error {
	t, err := template.New("network").Parse(`version: 2
ethernets: {{ range $idx, $int := .Interfaces }}
  eno{{ $idx }}:
    match:
      macaddress: "{{ $int.Id.MacString }}"
    set-name: eno{{ $idx }}
    dhcp4: yes
    dhcp6: yes
{{end}}
`)

	if err != nil {
		return errors.Wrapf(err, "formatting network config")
	}

	var buf bytes.Buffer

	err = t.ExecuteTemplate(&buf, "network", cfg)
	if err != nil {
		return err
	}

	w, err := iso9660.NewWriter()
	if err != nil {
		return err
	}

	defer w.Cleanup()

	data := buf.Bytes()

	err = w.AddFile(bytes.NewReader(data), "/network-config")
	if err != nil {
		return err
	}

	var v any
	if err = yaml.Unmarshal(data, &v); err != nil {
		return errors.Wrapf(err, "error detected in generated network-config")
	}

	if cfg.UserData != "" {
		err = w.AddFile(strings.NewReader(cfg.UserData), "/user-data")
		if err != nil {
			return err
		}
	}

	md := fmt.Sprintf(
		"instance-id: %s\nlocal-hostname: %s\n",
		cfg.Id.String(), cfg.Id.String(),
	)

	err = w.AddFile(strings.NewReader(md), "/meta-data")
	if err != nil {
		return err
	}

	f, err := os.Create(target)
	if err != nil {
		return err
	}

	defer f.Close()

	return w.WriteTo(f, "CIDATA")
}
