package manualqa

import "time"

type TypingSegment struct {
	LeadIn             time.Duration
	LeadOut            time.Duration
	KeypressMinLatency time.Duration
	KeypressMaxLatency time.Duration
	Input              []byte
}

type SoundFile struct {
	Name     string
	Duration time.Duration
}
