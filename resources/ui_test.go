package resources

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	loadedAsset = regexp.MustCompile(`<(?:script|link)[^>]+(?:src|href)="([^"]+)"`)
	assetsURL   = "{{.AppConfig.AssetsURL}}"
)

func TestUILoadsOnlyEmbeddedAssets(t *testing.T) {
	pages, err := fs.Glob(UIFS, "ui/*.html")
	require.NoError(t, err)
	require.NotEmpty(t, pages)

	for _, page := range pages {
		body, err := UIFS.ReadFile(page)
		require.NoError(t, err)

		for _, match := range loadedAsset.FindAllStringSubmatch(string(body), -1) {
			ref := match[1]
			assert.False(t, strings.Contains(ref, "://"), "%s loads %s from another host", page, ref)

			if !strings.HasPrefix(ref, assetsURL) {
				continue
			}
			name, _, _ := strings.Cut(strings.TrimPrefix(ref, assetsURL), "?")
			_, err := fs.Stat(UIFS, "ui/"+name)
			assert.NoError(t, err, "%s loads %s, which is not embedded", page, ref)
		}
	}
}
