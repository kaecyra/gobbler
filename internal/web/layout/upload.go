package layout

import (
	"errors"
	"io"
	"net/http"

	"github.com/kaecyra/gobbler/internal/media"
)

// MultipartOverhead is what a multipart body may add to the picture itself.
const MultipartOverhead = 64 << 10

// ReadUpload reads the picture in the multipart field named field, stopping
// past media.MaxUploadBytes rather than buffering what the server will refuse
// (ADR-00033). A body the person must fix (no picture, too large, unreadable)
// is a *media.Refusal. The avatar and the recipe picture uploads share it.
func ReadUpload(w http.ResponseWriter, r *http.Request, field string) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+MultipartOverhead)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, &media.Refusal{Msg: "Choose a picture to upload."}
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, &media.Refusal{Msg: "Choose a picture to upload."}
		}
		if err != nil {
			return nil, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
		}
		if part.FormName() != field {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, media.MaxUploadBytes+1))
		if err != nil {
			return nil, tooLargeOr(err, &media.Refusal{Msg: "The upload could not be read. Try again."})
		}
		return data, nil
	}
}

func tooLargeOr(err error, otherwise error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return &media.Refusal{Msg: "That file is over 5 MB. Choose a smaller picture."}
	}
	return otherwise
}
