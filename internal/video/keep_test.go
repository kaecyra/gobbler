package video

import (
	"context"
	"errors"
	"testing"
)

type keepStore struct{ saved map[string][]byte }

func (m *keepStore) HasThumbnail(_ context.Context, v string) (bool, error) {
	_, ok := m.saved[v]
	return ok, nil
}

func (m *keepStore) SaveThumbnail(_ context.Context, v string, c []byte, _ string) error {
	m.saved[v] = c
	return nil
}

type stubSource struct {
	asked []string
	fail  error
}

func (s *stubSource) Fetch(_ context.Context, v string) (Thumbnail, error) {
	s.asked = append(s.asked, v)
	if s.fail != nil {
		return Thumbnail{}, s.fail
	}
	return Thumbnail{Content: []byte("jpeg"), ContentType: "image/jpeg"}, nil
}

func TestKeepThumbnailDownloadsOnceAndSkipsNoVideo(t *testing.T) {
	st, src := &keepStore{saved: map[string][]byte{}}, &stubSource{}
	KeepThumbnail(context.Background(), st, src, 1, "")
	if len(src.asked) != 0 {
		t.Fatalf("downloaded %v for a recipe with no video", src.asked)
	}
	KeepThumbnail(context.Background(), st, src, 1, id)
	KeepThumbnail(context.Background(), st, src, 2, id)
	if string(st.saved[id]) != "jpeg" || len(src.asked) != 1 {
		t.Errorf("saved %q after downloads %v, want one download kept", st.saved[id], src.asked)
	}
}

func TestKeepThumbnailLogsAFailureAndRetriesOnTheNextSave(t *testing.T) {
	st, src := &keepStore{saved: map[string][]byte{}}, &stubSource{fail: errors.New("unreachable")}
	KeepThumbnail(context.Background(), st, src, 1, id)
	if len(st.saved) != 0 {
		t.Fatal("stored a thumbnail after a failed download")
	}
	src.fail = nil
	KeepThumbnail(context.Background(), st, src, 1, id)
	if len(st.saved) != 1 || len(src.asked) != 2 {
		t.Errorf("saved %d after downloads %v, want the retry to store it", len(st.saved), src.asked)
	}
}
