package usersignal

type Segment string

const (
	SegmentNewUser            Segment = "new_user"
	SegmentActiveSaver        Segment = "active_saver"
	SegmentAtRisk             Segment = "at_risk"
	SegmentDormant            Segment = "dormant"
	SegmentHighValue          Segment = "high_value"
)
