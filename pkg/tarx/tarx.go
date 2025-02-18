package tarx

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"

	"github.com/tonistiigi/fsutil"
)

func MakeTar(dir string) (io.Reader, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	gzw := gzip.NewWriter(w)
	tw := tar.NewWriter(gzw)

	go func() {
		defer w.Close()
		defer tw.Close()
		defer gzw.Close()

		// tar up dir and output it to tw
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}

			if dir == path {
				return nil
			}

			rp, _ := filepath.Rel(dir, path)

			hdr.Name = rp

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()

				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}

			return nil
		})
	}()

	return r, nil
}

func TarToMap(r io.Reader) (map[string][]byte, error) {
	tr := tar.NewReader(r)

	m := make(map[string][]byte)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}

		if th == nil {
			break
		}

		if th.Typeflag != tar.TypeReg {
			continue
		}

		buf := make([]byte, th.Size)
		n, err := io.ReadFull(tr, buf)
		if n == 0 && err != nil && err != io.EOF {
			return nil, err
		}

		m[th.Name] = buf[:n]
	}

	return m, nil
}

func TarFS(r io.Reader, dir string) (fsutil.FS, error) {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(gzr)

	for {
		th, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		path := filepath.Join(dir, th.Name)
		if th.Typeflag == tar.TypeDir {
			if err := os.Mkdir(path, 0755); err != nil {
				return nil, err
			}
		}

		if th.Typeflag == tar.TypeReg {
			f, err := os.Create(path)
			if err != nil {
				return nil, err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return nil, err
			}

			f.Chmod(os.FileMode(th.FileInfo().Mode()) & os.ModePerm)
		}
	}

	return fsutil.NewFS(dir)
}
