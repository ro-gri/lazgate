package server

import (
	"io/fs"
	"net/http"
	"os"

	webui "laz/internal/common/web"
	publicapp "laz/internal/transport/http/public"

	"github.com/go-chi/chi/v5"
)

func (s *Server) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(publicapp.NoIndexMiddleware)
	router.NotFound(publicapp.NotFound)
	publicapp.New(publicapp.Config{BlankPageHTML: s.blankPageHTML}).Mount(router)
	s.AdminApp().Mount(router)
	s.ClientApp().Mount(router)
	return router
}

func (s *Server) webAssets() http.Handler {
	return http.StripPrefix(s.webPrefix+"/assets/", http.FileServer(http.FS(multiFS{
		mustSubFS(webui.AssetsFS, "admin"),
		mustSubFS(webui.AssetsFS, "common"),
	})))
}

func (s *Server) connectAssets() http.Handler {
	return http.StripPrefix("/connect-assets/", http.FileServer(http.FS(multiFS{
		mustSubFS(webui.AssetsFS, "client"),
		mustSubFS(webui.AssetsFS, "common"),
	})))
}

func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

type multiFS []fs.FS

func (m multiFS) Open(name string) (fs.File, error) {
	for _, fsys := range m {
		file, err := fsys.Open(name)
		if err == nil {
			return file, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fs.ErrNotExist
}
