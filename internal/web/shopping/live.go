package shopping

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	domain "github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// fetchHeader marks a check sent by the page script, which wants no page back.
const fetchHeader = "X-Requested-With"

func (h *handler) live(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	view, err := h.Lists.Load(r.Context(), id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}
	aisles, err := newAisles(view.Aisles)
	if err != nil {
		fail(w, r, err)
		return
	}
	layout.Render(w, r, http.StatusOK, livePage(liveView{ID: id, Name: view.List.Name, Aisles: aisles}))
}

// check sets or clears the check on one item. The result reaches every open
// view through the store's change notification, so the answer carries no page.
func (h *handler) check(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !parseForm(w, r) {
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("key"))
	if key == "" {
		fail(w, r, fmt.Errorf("check without an item key: %w", layout.ErrInvalid))
		return
	}
	checked := r.PostForm.Get("checked") == "1"
	if err := h.Lists.SetChecked(r.Context(), id, domain.ItemKey(key), checked); err != nil {
		fail(w, r, fmt.Errorf("set check on list %d: %w", id, err))
		return
	}
	if r.Header.Get(fetchHeader) != "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, listURL(id)+"/live", http.StatusSeeOther)
}

// events streams the list to a live view: the whole list on connect, and again
// after every change. A client that reconnects after a drop therefore starts
// from the full current state, whatever it missed.
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	id, err := listID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	ctx := r.Context()
	// Subscribe before the first load, so a change between the two is not lost.
	sub, unsubscribe := h.hub.subscribe(id)
	defer unsubscribe()
	view, err := h.Lists.Load(ctx, id)
	if err != nil {
		fail(w, r, fmt.Errorf("load list %d: %w", id, err))
		return
	}

	// The server's write timeout is one deadline for the whole response, which
	// would cut a stream meant to stay open. Replace it with a deadline that
	// rolls forward before every write: a healthy client never sees it, and a
	// client that stopped reading is dropped after two heartbeats instead of
	// holding the stream until TCP gives up.
	rc := http.NewResponseController(w)
	warned := false
	roll := func() {
		err := rc.SetWriteDeadline(time.Now().Add(2 * h.heartbeat))
		if err != nil && !errors.Is(err, http.ErrNotSupported) && !warned {
			warned = true
			h.log.WarnContext(ctx, "set write deadline for event stream", "error", err.Error())
		}
	}
	roll()
	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(frame []byte) bool {
		roll()
		if _, err := w.Write(frame); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !h.sendList(ctx, id, view, send) {
		return
	}
	tick := time.NewTicker(h.heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !send([]byte(": keepalive\n\n")) {
				return
			}
		case <-sub.C:
			view, err := h.Lists.Load(ctx, id)
			if errors.Is(err, domain.ErrNotFound) {
				send([]byte("event: gone\ndata: \n\n"))
				return
			}
			if err != nil {
				h.log.ErrorContext(ctx, "reload list for event stream", "list_id", id, "error", err.Error())
				return
			}
			if !h.sendList(ctx, id, view, send) {
				return
			}
		}
	}
}

// sendList renders the list as one "list" event. It reports false when the
// stream should end.
func (h *handler) sendList(ctx context.Context, id int64, view domain.View, send func([]byte) bool) bool {
	aisles, err := newAisles(view.Aisles)
	if err != nil {
		h.log.ErrorContext(ctx, "render list for event stream", "list_id", id, "error", err.Error())
		return false
	}
	var html bytes.Buffer
	if err := itemsFragment(id, aisles, true).Render(ctx, &html); err != nil {
		h.log.ErrorContext(ctx, "render list for event stream", "list_id", id, "error", err.Error())
		return false
	}
	return send(eventFrame("list", html.String()))
}

// lineBreaks turns every line ending the event-stream format knows into \n.
var lineBreaks = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// eventFrame formats one server-sent event. Each line of data gets its own
// "data:" field, which the browser joins back with newlines. The format ends a
// line at \r, \r\n or \n, so all three are split on; a \r left in a field would
// cut the frame in the middle of the item's text.
func eventFrame(name, data string) []byte {
	var b bytes.Buffer
	b.WriteString("event: " + name + "\n")
	for _, line := range strings.Split(lineBreaks.Replace(data), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteByte('\n')
	return b.Bytes()
}
