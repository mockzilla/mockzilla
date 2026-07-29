package overlay

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	setupDir := filepath.Join(root, "v71", "setup")
	sharedDir := filepath.Join(root, "setup")
	for _, dir := range []string{setupDir, sharedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	local := filepath.Join(setupDir, "overlay.yml")
	if err := os.WriteFile(local, []byte("local: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(sharedDir, "overlay.yml")
	if err := os.WriteFile(shared, []byte("shared: yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("remote: yes\n"))
	}))
	defer srv.Close()

	remote := srv.URL + "/overlay.yml"

	tests := []struct {
		name    string
		sources []string
		want    []Source
	}{
		{
			name:    "relative to setup dir",
			sources: []string{"overlay.yml"},
			want: []Source{
				{Name: "overlay.yml", Path: local, Literal: "`local: yes\n`"},
			},
		},
		{
			name:    "shared overlay above the setup dir",
			sources: []string{"../../setup/overlay.yml", "overlay.yml"},
			want: []Source{
				{Name: "../../setup/overlay.yml", Path: shared, Literal: "`shared: yes\n`"},
				{Name: "overlay.yml", Path: local, Literal: "`local: yes\n`"},
			},
		},
		{
			name:    "absolute path",
			sources: []string{shared},
			want: []Source{
				{Name: shared, Path: shared, Literal: "`shared: yes\n`"},
			},
		},
		{
			name:    "url",
			sources: []string{remote},
			want: []Source{
				{Name: remote, Path: remote, Literal: "`remote: yes\n`"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(setupDir, tt.sources)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveMissingSource(t *testing.T) {
	_, err := Resolve(t.TempDir(), []string{"absent.yml"})
	if err == nil {
		t.Fatal("expected an error for a missing overlay")
	}
	if got := err.Error(); !strings.Contains(got, "absent.yml") {
		t.Errorf("error %q does not name the configured source", got)
	}
}

func TestGoStringLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "raw string", in: "overlay: 1.0.0\n", want: "`overlay: 1.0.0\n`"},
		{name: "backquote falls back to quoted", in: "a: `b`", want: `"a: ` + "`b`" + `"`},
		{name: "carriage return falls back to quoted", in: "a: b\r\n", want: `"a: b\r\n"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goStringLiteral(tt.in); got != tt.want {
				t.Errorf("goStringLiteral(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}
