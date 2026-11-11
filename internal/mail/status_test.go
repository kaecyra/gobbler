package mail

import "testing"

func TestStatusLine(t *testing.T) {
	for _, tc := range []struct {
		sent, err, want string
		failed          bool
	}{
		{"", "", "Not emailed", false},
		{"2026-11-10 09:00 UTC", "", "Emailed 2026-11-10 09:00 UTC", false},
		{"", "refused", "Email failed: refused", true},
		{"2026-11-10 09:00 UTC", "refused", "Email failed: refused (last sent 2026-11-10 09:00 UTC)", true},
	} {
		if got, failed := StatusLine(tc.sent, tc.err); got != tc.want || failed != tc.failed {
			t.Errorf("StatusLine(%q, %q) = %q, %v; want %q, %v", tc.sent, tc.err, got, failed, tc.want, tc.failed)
		}
	}
}
