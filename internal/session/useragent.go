package session

import "strings"

// Device is the browser and operating system a user agent names, in words a
// person recognises. Either is empty when the user agent does not say.
type Device struct {
	Browser string
	OS      string
}

// String reads "Firefox on Linux", or whichever half is known, or "Unknown
// browser".
func (d Device) String() string {
	switch {
	case d.Browser != "" && d.OS != "":
		return d.Browser + " on " + d.OS
	case d.Browser != "":
		return d.Browser
	case d.OS != "":
		return d.OS
	}
	return "Unknown browser"
}

// ParseUserAgent names the browser and operating system of a user agent. It
// recognises the common ones only: the string is the client's to write, so
// the result is a label for the person, not a fact to act on. The order of the
// checks matters, because most browsers claim to be several others.
func ParseUserAgent(ua string) Device {
	return Device{Browser: browserOf(ua), OS: osOf(ua)}
}

func browserOf(ua string) string {
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"EdgiOS/", "Edge"},
		{"OPR/", "Opera"}, {"Opera", "Opera"},
		{"Vivaldi/", "Vivaldi"},
		{"SamsungBrowser/", "Samsung Internet"},
		{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
		{"curl/", "curl"},
	} {
		if strings.Contains(ua, b.token) {
			return b.name
		}
	}
	return ""
}

func osOf(ua string) string {
	for _, o := range []struct{ token, name string }{
		{"Windows", "Windows"},
		{"Android", "Android"},
		{"iPhone", "iOS"}, {"iPad", "iPadOS"},
		{"Mac OS X", "macOS"}, {"Macintosh", "macOS"},
		{"CrOS", "ChromeOS"},
		{"Linux", "Linux"}, {"X11", "Linux"},
	} {
		if strings.Contains(ua, o.token) {
			return o.name
		}
	}
	return ""
}
