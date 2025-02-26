package runpkg

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"
)

func CopyFS(dir string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		targ := filepath.Join(dir, filepath.FromSlash(path))
		if d.IsDir() {
			if err := os.MkdirAll(targ, 0777); err != nil {
				return err
			}
			return nil
		}

		r, err := fsys.Open(path)
		if err != nil {
			return err
		}

		defer r.Close()
		info, err := r.Stat()
		if err != nil {
			return err
		}

		w, err := os.OpenFile(targ, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666|info.Mode()&0777)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return fmt.Errorf("copying %s: %v", path, err)
		}

		if err := w.Close(); err != nil {
			return err
		}
		return nil
	})
}

func Expand(log *slog.Logger, rp *RunPkg, target string) error {
	zr, err := zip.NewReader(bytes.NewReader(rp.data), int64(len(rp.data)))
	if err != nil {
		return err
	}

	f, err := zr.Open("config.yml")
	if err != nil {
		return err
	}

	cfg, err := ParseConfig(f)
	if err != nil {
		return err
	}

	cfgPath := filepath.Join(target, "config")

	err = os.Mkdir(cfgPath, 0755)
	if err != nil {
		return err
	}

	for _, cf := range cfg.Configs {
		if cf.TemplateFile != "" {
			err := func() error {
				g, err := zr.Open(cf.TemplateFile)
				if err != nil {
					return err
				}

				defer g.Close()

				data, err := io.ReadAll(g)
				if err != nil {
					return err
				}

				t, err := template.New("masq").Parse(string(data))
				if err != nil {
					return err
				}

				out, err := os.Create(filepath.Join(cfgPath, cf.Name))
				if err != nil {
					return err
				}

				defer out.Close()

				err = t.ExecuteTemplate(out, cf.Name, cf.Data)
				if err != nil {
					return err
				}

				return nil
			}()

			if err != nil {
				return err
			}
		}
	}

	err = CopyFS(target, zr)
	if err != nil {
		return err
	}

	return nil
}
