package layout

import (
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/kaecyra/gobbler/internal/media"
)

// MultipartOverhead is what a multipart body may add to the picture itself.
const MultipartOverhead = 64 << 10

// ReadUpload reads the picture in the multipart field named field, stopping
// past media.MaxUploadBytes rather than buffering what the server will refuse
// (ADR-00033). A body the person must fix (no picture, too large, unreadable)
// is a *media.Refusal. The avatar and the recipe picture uploads share it.
func ReadUpload(w http.ResponseWriter, r *http.Request, field string) ([]byte, error) {
	data, _, err := ReadUploadWith(w, r, field)
	return data, err
}

// maxUploadValue bounds a short field read beside an upload.
const maxUploadValue = 1 << 10

// ReadUploadWith is ReadUpload that also returns the short text fields named
// in extra, read in the order the parts come. The body is cut off past the
// upload limit, so a field that comes after a file over the limit is never
// reached: put such a field ahead of the file. The fields read before a
// failure are returned with it. A field longer than maxUploadValue, or absent
// or unreached, is empty.
func ReadUploadWith(w http.ResponseWriter, r *http.Request, field string, extra ...string) ([]byte, map[string]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+MultipartOverhead)
	values := map[string]string{}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, values, &media.Refusal{Msg: "Choose a picture to upload."}
	}
	var data []byte
	found := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, values, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
		}
		switch name := part.FormName(); {
		case name == field && !found:
			if data, err = io.ReadAll(io.LimitReader(part, media.MaxUploadBytes+1)); err != nil {
				return nil, values, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
			}
			found = true
		case slices.Contains(extra, name):
			v, err := io.ReadAll(io.LimitReader(part, maxUploadValue+1))
			if err != nil {
				return nil, values, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
			}
			if len(v) <= maxUploadValue {
				values[name] = string(v)
			}
		}
	}
	if !found {
		return nil, values, &media.Refusal{Msg: "Choose a picture to upload."}
	}
	return data, values, nil
}

func tooLargeOr(err error, otherwise error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return &media.Refusal{Msg: "That file is over 5 MB. Choose a smaller picture."}
	}
	return otherwise
}
