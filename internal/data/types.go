package data

import "time"

// CallSummary is one logical call (one linkedid).
type CallSummary struct {
	LinkedID             string
	CallDate             time.Time
	Src                  string
	Clid                 string
	Cnum                 string
	Cnam                 string
	Dst                  string
	Direction            string
	Disposition          string
	DurationSeconds      int32
	BillsecSeconds       int32
	PickupSeconds        *int32
	AnsweredExtension    string
	OriginatingExtension string
	DID                  string
	LegCount             int32
	RecordingFile        string
}

// CallLeg is one row from cdr.
type CallLeg struct {
	Uniqueid        string
	CallDate        time.Time
	Channel         string
	Dstchannel      string
	Src             string
	Dst             string
	Lastapp         string
	Lastdata        string
	Disposition     string
	DurationSeconds int32
	BillsecSeconds  int32
	Extension       string
	RecordingFile   string
	// RTPQoS is parsed from the cdr.rtpqos column; nil when the column
	// is empty (call not bridged, or no RTCP report received).
	RTPQoS *RTPQoS
}

// CelEvent is one row from cel.
type CelEvent struct {
	EventTime time.Time
	EventType string
	ChanName  string
	Uniqueid  string
	AppName   string
	AppData   string
	CidName   string
	CidNum    string
	Exten     string
	Context   string
}

// CallFilter narrows ListCalls.
type CallFilter struct {
	From        time.Time
	To          time.Time
	Src         string
	Dst         string
	Extension   string
	Disposition string // "" = all, otherwise raw cdr.disposition value
	Direction   string // "" = all, "inbound" | "outbound" | "internal"
	Page        int
	PageSize    int
}

// OverviewFilter narrows the dataset for stats.Overview.
type OverviewFilter struct {
	From   time.Time
	To     time.Time
	Bucket BucketGranularity
}

// ExtensionStatsFilter narrows ListExtensionStats.
type ExtensionStatsFilter struct {
	From      time.Time
	To        time.Time
	Extension string
	Page      int
	PageSize  int
}

// BucketGranularity selects the histogram resolution.
type BucketGranularity int

const (
	BucketNone BucketGranularity = iota
	BucketHour
	BucketDay
	BucketWeek
)

// TimeBucketCount is one bin in a histogram series.
type TimeBucketCount struct {
	BucketStart time.Time
	Total       int32
	Answered    int32
	Missed      int32
}

// OverviewResult is what StatsRepo.Overview returns.
type OverviewResult struct {
	TotalCalls        int32
	AnsweredCalls     int32
	MissedCalls       int32
	BusyCalls         int32
	FailedCalls       int32
	AvgPickupSeconds  float64
	AvgTalkSeconds    float64
	Series            []TimeBucketCount
}

// ExtensionStat is one row in ListExtensionStats.
type ExtensionStat struct {
	Extension        string
	DisplayName      string
	TotalCalls       int32
	AnsweredCalls    int32
	MissedCalls      int32
	InboundCalls     int32
	OutboundCalls    int32
	TotalTalkSeconds int32
	HandledShare     float64
	AvgPickupSeconds float64
	AvgTalkSeconds   float64
	BusiestHour      int32
}

// ExtensionDrilldown is what GetExtensionStats returns.
type ExtensionDrilldown struct {
	Summary   ExtensionStat
	Series    []TimeBucketCount
	HourOfDay []TimeBucketCount
}

// RingGroupStatsFilter narrows RingGroupStats.
type RingGroupStatsFilter struct {
	RingGroup string
	From      time.Time
	To        time.Time
}

// MissedRingGroupCall is one inbound call that did not get answered by any
// member of the ringgroup.
type MissedRingGroupCall struct {
	LinkedID     string
	CallDate     time.Time
	Src          string
	Clid         string
	DID          string
	Disposition  string // NO ANSWER | BUSY | FAILED
	RingSeconds  int32
}

// RingGroupStatsResult is what RingGroupStats returns.
type RingGroupStatsResult struct {
	RingGroup    string
	Total        int32
	Answered     int32
	NoAnswer     int32
	AllBusy      int32
	Failed       int32
	MissedCalls  []MissedRingGroupCall
}

// PJSIPRegStatus enumerates the ContactStatus values Asterisk emits via AMI.
// See https://docs.asterisk.org/Latest_API/API_Documentation/AMI_Events/ContactStatus/
type PJSIPRegStatus string

const (
	PJSIPRegCreated     PJSIPRegStatus = "Created"
	PJSIPRegRemoved     PJSIPRegStatus = "Removed"
	PJSIPRegReachable   PJSIPRegStatus = "Reachable"
	PJSIPRegUnreachable PJSIPRegStatus = "Unreachable"
	PJSIPRegUpdated     PJSIPRegStatus = "Updated"
	PJSIPRegUnknown     PJSIPRegStatus = "Unknown"
	PJSIPRegUnqualified PJSIPRegStatus = "Unqualified"
)

// IsRegistered reports whether a status implies the contact was usable.
func (s PJSIPRegStatus) IsRegistered() bool {
	switch s {
	case PJSIPRegCreated, PJSIPRegReachable, PJSIPRegUpdated, PJSIPRegUnqualified:
		return true
	default:
		return false
	}
}

// PJSIPRegEvent is one ContactStatus AMI event captured by the listener and
// persisted to the tangra DB.
type PJSIPRegEvent struct {
	ID         int64
	EventTime  time.Time
	Endpoint   string
	AOR        string
	ContactURI string
	Status     PJSIPRegStatus
	UserAgent  string
	ViaAddress string
	RegExpire  *time.Time
	RTTMicros  int64
}

// PJSIPRegStatusAt is the inferred state of an extension at a point in time,
// derived from the most recent prior event.
type PJSIPRegStatusAt struct {
	Endpoint    string
	Registered  bool
	Status      PJSIPRegStatus // empty when no events exist
	LastEvent   *PJSIPRegEvent
}

// PJSIPRegEventFilter narrows ListPJSIPRegistrationEvents.
type PJSIPRegEventFilter struct {
	Endpoint string // optional substring match
	From     time.Time
	To       time.Time
	Page     int
	PageSize int
}

// PJSIPRegisteredEndpoint is one extension that was registered at a given
// instant — i.e. its most recent event before that instant left it in a
// reachable/registered state. Used by "who was online at call time?".
type PJSIPRegisteredEndpoint struct {
	Endpoint      string
	ContactURI    string
	UserAgent     string
	ViaAddress    string
	Status        PJSIPRegStatus
	LastEventTime time.Time
	RegExpire     *time.Time
}
