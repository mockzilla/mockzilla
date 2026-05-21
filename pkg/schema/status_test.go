package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinResponseCodes(t *testing.T) {
	cases := []struct {
		name  string
		codes []int
		want2 int
		want3 int
		want4 int
		want5 int
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"all buckets", []int{201, 200, 301, 302, 404, 401, 503, 500}, 200, 301, 401, 500},
		{"only 5xx", []int{503, 500}, 0, 0, 0, 500},
		{"below 200 ignored", []int{100, 199, 200}, 200, 0, 0, 0},
		{"single 2xx", []int{204}, 204, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got2, got3, got4, got5 := MinResponseCodes(c.codes)
			assert.Equal(t, c.want2, got2)
			assert.Equal(t, c.want3, got3)
			assert.Equal(t, c.want4, got4)
			assert.Equal(t, c.want5, got5)
		})
	}
}
