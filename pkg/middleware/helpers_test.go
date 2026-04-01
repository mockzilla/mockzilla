package middleware

import (
	"net/http"
	"time"

	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/db"
)

// BufferedWriter is a writer that captures the response.
// Used to capture the template execution result.
type BufferedWriter struct {
	buf        []byte
	statusCode int
	header     http.Header
}

// NewBufferedResponseWriter creates a new buffered writer.
func NewBufferedResponseWriter() *BufferedWriter {
	return &BufferedWriter{
		buf:    make([]byte, 0, 1024),
		header: make(http.Header),
	}
}

// Write writes the data to the buffer.
func (bw *BufferedWriter) Write(p []byte) (int, error) {
	bw.buf = append(bw.buf, p...)
	return len(p), nil
}

// Header returns the header.
func (bw *BufferedWriter) Header() http.Header {
	return bw.header
}

// WriteHeader writes the status code.
func (bw *BufferedWriter) WriteHeader(statusCode int) {
	bw.statusCode = statusCode
}

// newTestParams creates a new Params with a memory DB for testing.
func newTestParams(serviceCfg *config.ServiceConfig) *Params {
	if serviceCfg == nil {
		serviceCfg = &config.ServiceConfig{Name: "test"}
	}
	storage := db.NewStorage(nil)
	database := storage.NewDB(serviceCfg.Name, 100*time.Second)
	return NewParams(serviceCfg, database)
}

// waitForAsync gives background goroutines time to complete.
func waitForAsync() {
	time.Sleep(10 * time.Millisecond)
}
