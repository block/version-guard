package types

// Status represents the compliance status of a resource's version
type Status string

const (
	// StatusRed indicates critical issues: past EOL, deprecated, or extended support expired
	StatusRed Status = "RED"
	// StatusYellow indicates warnings: in extended/deprecated support or approaching EOL (< 90 days)
	StatusYellow Status = "YELLOW"
	// StatusGreen indicates compliant: current supported version
	StatusGreen Status = "GREEN"
	// StatusUnknown indicates version not found in EOL database
	StatusUnknown Status = "UNKNOWN"
)

// String returns the string representation of the Status
func (s Status) String() string {
	return string(s)
}

// IsValid returns true if the Status is a known value
func (s Status) IsValid() bool {
	switch s {
	case StatusRed, StatusYellow, StatusGreen, StatusUnknown:
		return true
	default:
		return false
	}
}

// Severity returns a numeric severity level for sorting (higher = more severe)
func (s Status) Severity() int {
	switch s {
	case StatusRed:
		return 3
	case StatusYellow:
		return 2
	case StatusGreen:
		return 1
	case StatusUnknown:
		return 0
	default:
		return -1
	}
}
