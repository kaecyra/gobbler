package layouttest

import "testing"

func TestRemoteAssetsFlagsEveryLoadingReference(t *testing.T) {
	cases := map[string]string{
		"script":        `<script src="https://cdn.example/x.js"></script>`,
		"script single": `<script src='//cdn.example/x.js'></script>`,
		"stylesheet":    `<link rel="stylesheet" href="http://f.example/a.css">`,
		"icon":          `<link rel="shortcut icon" href="https://f.example/i.ico">`,
		"preload":       `<link href="https://f.example/f.woff2" rel="preload" as="font">`,
		"img":           `<img src="https://i.example/a.png" alt="">`,
		"srcset":        `<img src="/a.png" srcset="/a.png 1x, https://i.example/b.png 2x">`,
		"source":        `<video><source src="https://v.example/a.mp4"></video>`,
		"iframe":        `<iframe src="https://e.example/embed"></iframe>`,
		"audio":         `<audio src="//a.example/s.mp3"></audio>`,
		"style block":   `<style>@font-face{font-family:x;src:url(https://f.example/x.woff2)}</style>`,
		"style import":  `<style>@import "https://f.example/a.css";</style>`,
		"style attr":    `<div style="background:url('https://i.example/a.png')"></div>`,
	}
	for name, html := range cases {
		t.Run(name, func(t *testing.T) {
			remote, _ := RemoteAssets(html)
			if len(remote) == 0 {
				t.Errorf("not flagged: %s", html)
			}
		})
	}
}

func TestRemoteAssetsAllowsLocalAssetsAndNavigationLinks(t *testing.T) {
	html := `<link rel="stylesheet" href="/static/app.css"><script src="/static/x.js"></script>` +
		`<link rel="canonical" href="https://example.org/a">` +
		`<a href="https://github.com/kaecyra/gobbler" target="_blank">repo</a>` +
		`<img src="/static/img/gob.svg"><div style="background:url(data:image/svg+xml;xmlns=http://www.w3.org/2000/svg)"></div>` +
		`<style>.a{background:url("/static/a.png")}</style>`
	remote, checked := RemoteAssets(html)
	if len(remote) != 0 {
		t.Errorf("flagged local or navigation references: %v", remote)
	}
	if checked == 0 {
		t.Error("examined nothing")
	}
}

func TestRemoteCSSFlagsUrlAndImport(t *testing.T) {
	for _, css := range []string{
		`@import url(https://f.example/a.css);`,
		`@import "//f.example/a.css";`,
		`a{background:url( "http://i.example/a.png" )}`,
	} {
		if remote, _ := RemoteCSS(css); len(remote) == 0 {
			t.Errorf("not flagged: %s", css)
		}
	}
	if remote, _ := RemoteCSS(`a{background:url(/x.png)} @import "/local.css";`); len(remote) != 0 {
		t.Errorf("flagged local css: %v", remote)
	}
}

func TestRemoteAssetsCountsNothingOnAPageWithNoAssets(t *testing.T) {
	if remote, checked := RemoteAssets("<p>hi</p>"); len(remote) != 0 || checked != 0 {
		t.Fatalf("remote=%v checked=%d", remote, checked)
	}
}
