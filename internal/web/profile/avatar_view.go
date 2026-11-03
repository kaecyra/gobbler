package profile

import (
	"strconv"

	"github.com/kaecyra/gobbler/internal/media"
	domain "github.com/kaecyra/gobbler/internal/profile"
)

// statusView is what the section's status fragment shows.
type statusView struct {
	Address string
	// State is the state of the upload in flight, empty when there is none.
	State string
	// Error is why the last attempt failed, for the person.
	Error string
}

func newStatusView(address string, j domain.Job, found bool) statusView {
	if !found {
		return statusView{Address: address}
	}
	return statusView{Address: address, State: j.State, Error: j.Error}
}

func (v statusView) statusPath() string { return v.path("avatar/status") }
func (v statusView) cropPath() string   { return v.path("avatar/crop") }
func (v statusView) path(section string) string {
	return Pattern + "/" + v.Address + "/" + section
}

// cropView is the crop page.
type cropView struct {
	Address string
	ID      string
	// Failed is set when the last attempt on this picture failed.
	Failed string
}

func newCropView(address string, j domain.Job) cropView {
	return cropView{Address: address, ID: j.ID, Failed: j.Error}
}

func (v cropView) path(section string) string {
	return Pattern + "/" + v.Address + "/" + section
}

// uploadAccept is the accept list of the file picker; the server decodes the
// file and decides regardless (ADR-00033).
const uploadAccept = "image/jpeg,image/png,image/gif,image/webp,image/heic,image/avif,.heic,.heif,.avif"

// maxUploadMB is the limit in megabytes, for the page text.
var maxUploadMB = strconv.Itoa(media.MaxUploadBytes >> 20)

// avatarSrcOf is the picture's address for the page.
func avatarSrcOf(v pageView) string { return AvatarSrc(v.Address, v.Shown.Avatar) }
