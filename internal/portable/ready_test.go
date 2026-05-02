package portable

import (
	"encoding/json"
	"testing"
)

func TestBuildReadyStamp(t *testing.T) {
	t.Run("sorts services and includes the home URL", func(t *testing.T) {
		line, err := buildReadyStamp(2200, "/", []string{"petstore", "adyen"})
		if err != nil {
			t.Fatalf("buildReadyStamp returned error: %v", err)
		}

		var got readyStamp
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("returned line is not valid JSON: %v (line=%q)", err, line)
		}

		if got.Status != "ready" {
			t.Errorf("status = %q, want %q", got.Status, "ready")
		}
		if got.Port != 2200 {
			t.Errorf("port = %d, want 2200", got.Port)
		}
		if got.URL != "http://localhost:2200/" {
			t.Errorf("url = %q, want http://localhost:2200/", got.URL)
		}
		want := []readyService{
			{Name: "adyen", Path: "/adyen"},
			{Name: "petstore", Path: "/petstore"},
		}
		if len(got.Services) != len(want) {
			t.Fatalf("services len = %d, want %d", len(got.Services), len(want))
		}
		for i, svc := range got.Services {
			if svc != want[i] {
				t.Errorf("service[%d] = %+v, want %+v", i, svc, want[i])
			}
		}
	})

	t.Run("works with no services", func(t *testing.T) {
		line, err := buildReadyStamp(3000, "/home", nil)
		if err != nil {
			t.Fatalf("buildReadyStamp returned error: %v", err)
		}
		var got readyStamp
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("returned line is not valid JSON: %v", err)
		}
		if len(got.Services) != 0 {
			t.Errorf("services = %+v, want empty", got.Services)
		}
		if got.URL != "http://localhost:3000/home" {
			t.Errorf("url = %q, want http://localhost:3000/home", got.URL)
		}
	})
}
