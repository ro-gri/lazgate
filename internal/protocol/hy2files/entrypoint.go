package hy2files

import "errors"

var ErrMissingIndex = errors.New("В корне набора должен быть файл index.html (/index.html). Файл во вложенной папке не подходит.")

func RequireIndex(paths []string) error {
	for _, name := range paths {
		if name == "index.html" {
			return nil
		}
	}
	return ErrMissingIndex
}
