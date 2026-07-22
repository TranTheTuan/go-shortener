package useragent

import ua "github.com/mileusna/useragent"

// Result holds the normalized device classification parsed from a User-Agent string.
type Result struct {
	Device  string // desktop|mobile|tablet|bot|unknown
	Browser string // e.g. "Chrome"; "" becomes "unknown"
	OS      string // e.g. "Windows"; "" becomes "unknown"
}

// Parse extracts device, browser, and OS from a User-Agent string.
// Empty or unrecognised input returns "unknown" for all fields.
func Parse(s string) Result {
	if s == "" {
		return Result{Device: "unknown", Browser: "unknown", OS: "unknown"}
	}
	p := ua.Parse(s)
	return Result{
		Device:  deviceClass(p),
		Browser: orUnknown(truncate(p.Name, 40)),
		OS:      orUnknown(truncate(p.OS, 40)),
	}
}

func deviceClass(p ua.UserAgent) string {
	switch {
	case p.Bot:
		return "bot"
	case p.Tablet:
		return "tablet"
	case p.Mobile:
		return "mobile"
	case p.Desktop:
		return "desktop"
	default:
		return "unknown"
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
}
