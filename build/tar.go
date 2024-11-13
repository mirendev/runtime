package build

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
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

		if th.Typeflag != tar.TypeReg {
			continue
		}

		if err != nil {
			return nil, err
		}

		buf := make([]byte, th.Size)
		n, err := tr.Read(buf)
		if n == 0 && err == io.EOF {
			return nil, err
		}

		m[th.Name] = buf[:n]
	}

	return m, nil
}
