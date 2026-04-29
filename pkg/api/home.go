package api

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/resources"
)

// CreateHomeRoutes creates routes for home.
func CreateHomeRoutes(router *Router) error {
	homeURL := router.Config().HomeURL
	trimmed := strings.Trim(homeURL, "/")

	var url string
	if trimmed == "" {
		url = "/"
	} else {
		url = "/" + trimmed + "/"
	}

	if url != "/" {
		homeRedirect := http.RedirectHandler(url, http.StatusMovedPermanently).ServeHTTP
		router.Get(strings.TrimSuffix(url, "/"), homeRedirect)
	}

	router.Get(url, createUIHandler(router))

	if trimmed == "" {
		docsServer("/docs/*", router)
		fileServer("/*", router)
	} else {
		docsServer(fmt.Sprintf("/%s/docs/*", trimmed), router)
		fileServer(fmt.Sprintf("/%s/*", trimmed), router)
	}

	return nil
}

// BufferedWriter is a writer that captures the response.
// Used to capture the template execution result.
type BufferedWriter struct {
	buf        []byte
	statusCode int
}

// NewBufferedResponseWriter creates a new buffered writer.
func NewBufferedResponseWriter() *BufferedWriter {
	return &BufferedWriter{
		buf: make([]byte, 0, 1024),
	}
}

// Write writes the data to the buffer.
func (bw *BufferedWriter) Write(p []byte) (int, error) {
	bw.buf = append(bw.buf, p...)
	return len(p), nil
}

// Header returns the header.
func (bw *BufferedWriter) Header() http.Header {
	return http.Header{}
}

// WriteHeader writes the status code.
func (bw *BufferedWriter) WriteHeader(statusCode int) {
	bw.statusCode = statusCode
}

// createUIHandler creates a handler function for home.
// Uses filesystem UI if available, otherwise falls back to embedded.
func createUIHandler(router *Router) http.HandlerFunc {
	uiPath := router.Config().Paths.UI
	useFilesystem := hasUIFiles(uiPath)

	return func(w http.ResponseWriter, r *http.Request) {
		var indexHTML []byte
		var err error

		if useFilesystem {
			indexHTML, err = os.ReadFile(filepath.Join(uiPath, "index.html"))
		} else {
			indexHTML, err = resources.UIFS.ReadFile("ui/index.html")
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tpl, err := template.New("index.html").Parse(string(indexHTML))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		cfg := router.Config()

		type TemplateData struct {
			AppConfig *config.AppConfig
			Contents  map[string]template.HTML
			Version   string
		}

		var homeContents []byte
		if useFilesystem {
			homeContents, err = os.ReadFile(filepath.Join(uiPath, "home.html"))
		} else {
			homeContents, err = resources.UIFS.ReadFile("ui/home.html")
		}
		if err != nil {
			log.Println("Failed to get home contents", err)
		}

		var headerContents []byte
		if useFilesystem {
			headerContents, _ = os.ReadFile(filepath.Join(uiPath, "header.html"))
		} else {
			headerContents, _ = resources.UIFS.ReadFile("ui/header.html")
		}

		var footerContents []byte
		if useFilesystem {
			footerContents, _ = os.ReadFile(filepath.Join(uiPath, "footer.html"))
		} else {
			footerContents, _ = resources.UIFS.ReadFile("ui/footer.html")
		}

		var stylesContents []byte
		if useFilesystem {
			stylesContents, _ = os.ReadFile(filepath.Join(uiPath, "styles.html"))
		} else {
			stylesContents, _ = resources.UIFS.ReadFile("ui/styles.html")
		}

		data := &TemplateData{
			AppConfig: cfg,
			Contents: map[string]template.HTML{
				"Home":   template.HTML(homeContents),
				"Header": template.HTML(headerContents),
				"Footer": template.HTML(footerContents),
				"Styles": template.HTML(stylesContents),
			},
			Version: os.Getenv("APP_VERSION"),
		}

		// Create a buffered writer to capture the template execution result.
		buf := NewBufferedResponseWriter()

		err = tpl.Execute(buf, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		SendHTML(w, http.StatusOK, buf.buf)
	}
}

// fileServer conveniently sets up a http.FileServer handler to serve
// static files. Uses filesystem UI if available, otherwise embedded.
func fileServer(url string, router *Router) {
	uiPath := router.Config().Paths.UI

	var fileSystem http.FileSystem
	if hasUIFiles(uiPath) {
		fileSystem = http.Dir(uiPath)
	} else {
		uiSubFS, err := fs.Sub(resources.UIFS, "ui")
		if err != nil {
			log.Printf("Failed to create UI sub-filesystem: %v", err)
			return
		}
		fileSystem = http.FS(uiSubFS)
	}

	router.Get(url, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		server := http.StripPrefix(pathPrefix, http.FileServer(fileSystem))
		server.ServeHTTP(w, r)
	})
}

// hasUIFiles checks if a UI directory exists with required files.
func hasUIFiles(uiPath string) bool {
	indexPath := filepath.Join(uiPath, "index.html")
	_, err := os.Stat(indexPath)
	return err == nil
}

// docsServer serves the docs assets.
func docsServer(url string, router *Router) {
	router.Get(url, func(w http.ResponseWriter, r *http.Request) {
		ufs := http.StripPrefix(
			strings.TrimSuffix(url, "/*"),
			http.FileServer(http.Dir(filepath.Join(router.Config().Paths.Base, "site"))))
		ufs.ServeHTTP(w, r)
	})
}
