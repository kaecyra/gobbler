package profile

import (
	"net/url"
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
	// Has is the avatar key the page was drawn with, echoed back on each poll.
	Has string
}

func newStatusView(address string, j domain.Job, found bool, has string) statusView {
	if !found {
		return statusView{Address: address, Has: has}
	}
	return statusView{Address: address, State: j.State, Error: j.Error, Has: has}
}

// statusPath is the status fragment's address, carrying the avatar the page
// shows so the server can tell when the page is out of date.
func (v statusView) statusPath() string {
	return v.path("avatar/status") + "?has=" + url.QueryEscape(v.Has)
}

// panelView is the upload step of the dialog.
type panelView struct {
	Address string
	// Has is the avatar key the page was drawn with.
	Has string
	// Message is why the last upload or crop was refused, for the person.
	Message string
}

func newPanelView(address, has, message string) panelView {
	return panelView{Address: address, Has: has, Message: message}
}

func (v panelView) path(section string) string { return Pattern + "/" + v.Address + "/" + section }

// statusSrc is the status fragment's address for the step's first request.
func (v panelView) statusSrc() string {
	return v.path("avatar/status") + "?has=" + url.QueryEscape(v.Has)
}
func (v statusView) cropPath() string { return v.path("avatar/crop") }
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
