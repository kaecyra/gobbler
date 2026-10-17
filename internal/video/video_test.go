package video

import (
	"errors"
	"testing"
)

const id = "dQw4w9WgXcQ"

func TestParseID(t *testing.T) {
	accepted := []struct{ name, in string }{
		{"bare id", id},
		{"bare id with spaces around", "  " + id + "  "},
		{"watch", "https://www.youtube.com/watch?v=" + id},
		{"watch without www", "https://youtube.com/watch?v=" + id},
		{"watch with other parameters first", "https://www.youtube.com/watch?feature=share&v=" + id + "&t=42s"},
		{"watch on http", "http://www.youtube.com/watch?v=" + id},
		{"watch without scheme", "youtube.com/watch?v=" + id},
		{"watch on the mobile host", "https://m.youtube.com/watch?v=" + id},
		{"watch on the music host", "https://music.youtube.com/watch?v=" + id},
		{"upper-case host", "https://WWW.YouTube.com/watch?v=" + id},
		{"short link", "https://youtu.be/" + id},
		{"short link with tracking", "https://youtu.be/" + id + "?si=abc123"},
		{"short link without scheme", "youtu.be/" + id},
		{"shorts", "https://www.youtube.com/shorts/" + id},
		{"shorts with query", "https://youtube.com/shorts/" + id + "?feature=share"},
		{"embed", "https://www.youtube.com/embed/" + id},
		{"nocookie embed", "https://www.youtube-nocookie.com/embed/" + id},
		{"nocookie embed with autoplay", "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1"},
	}
	for _, c := range accepted {
		t.Run("accepts "+c.name, func(t *testing.T) {
			got, err := ParseID(c.in)
			if err != nil || got != id {
				t.Fatalf("ParseID(%q) = %q, %v; want %q", c.in, got, err, id)
			}
		})
	}

	t.Run("accepts the dash and underscore id as itself", func(t *testing.T) {
		if got, err := ParseID("a-b_c-d_e-f"); err != nil || got != "a-b_c-d_e-f" {
			t.Fatalf("got %q, %v", got, err)
		}
	})

	for _, in := range []string{"", "   ", "\t\n"} {
		if got, err := ParseID(in); err != nil || got != "" {
			t.Errorf("ParseID(%q) = %q, %v; want empty and no error (no video)", in, got, err)
		}
	}

	rejected := []struct{ name, in string }{
		{"free text", "my favourite pancake video"},
		{"too short id", "abc"},
		{"too long bare id", id + "x"},
		{"bare id with a bad character", "dQw4w9WgXc!"},
		{"another host", "https://vimeo.com/123456789"},
		{"a look-alike host", "https://youtube.com.evil.example/watch?v=" + id},
		{"a host that only ends like youtube", "https://notyoutube.com/watch?v=" + id},
		{"userinfo trick", "https://www.youtube.com@evil.example/watch?v=" + id},
		{"a port", "https://www.youtube.com:8443/watch?v=" + id},
		{"a non-web scheme", "ftp://www.youtube.com/watch?v=" + id},
		{"javascript scheme", "javascript:alert(1)"},
		{"watch with no v", "https://www.youtube.com/watch"},
		{"watch with a short v", "https://www.youtube.com/watch?v=abc"},
		{"watch with a long v", "https://www.youtube.com/watch?v=" + id + "extra"},
		{"channel page", "https://www.youtube.com/@somechannel"},
		{"playlist page", "https://www.youtube.com/playlist?list=PL1234567890A"},
		{"youtube root", "https://www.youtube.com/"},
		{"short link with no id", "https://youtu.be/"},
		{"short link with a path after the id", "https://youtu.be/" + id + "/more"},
		{"shorts with a bad id", "https://www.youtube.com/shorts/abc"},
		{"embed on a lookalike", "https://www.youtube-nocookie.com.evil.example/embed/" + id},
		{"nocookie watch", "https://www.youtube-nocookie.com/watch?v=" + id},
	}
	for _, c := range rejected {
		t.Run("rejects "+c.name, func(t *testing.T) {
			got, err := ParseID(c.in)
			if !errors.Is(err, ErrInvalidLink) || got != "" {
				t.Fatalf("ParseID(%q) = %q, %v; want ErrInvalidLink", c.in, got, err)
			}
		})
	}
}

func TestEmbedURLIsPrivacyEnhancedAndAutoplays(t *testing.T) {
	got := EmbedURL(id)
	want := "https://www.youtube-nocookie.com/embed/" + id + "?autoplay=1&rel=0"
	if got != want {
		t.Fatalf("EmbedURL = %q, want %q", got, want)
	}
}
