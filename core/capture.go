package core

import "fmt"

// SourceKind identifies where a Capture came from.
type SourceKind string

const (
	SourceSCPI      SourceKind = "scpi"
	SourceFile      SourceKind = "file"
	SourceSynthetic SourceKind = "synthetic"
)

// SourceInfo describes a capture's provenance. It is metadata only and never
// substitutes for a sample time coordinate.
type SourceInfo struct {
	Kind       SourceKind
	Vendor     string
	Model      string
	Firmware   string
	Resource   string
	FilePath   string
	FileFormat string
}

// Record is one channel of one acquisition segment.
type Record struct {
	Channel      string
	SegmentIndex int
	Waveform     Waveform
	Acquisition  string
	Diagnostics  []Diagnostic
}

// Capture is a set of records from one source. Records are not concatenated
// across segments; SegmentIndex identifies the acquisition segment.
type Capture struct {
	Source      SourceInfo
	Records     []Record
	Diagnostics []Diagnostic
}

// Channel returns the record for a channel and segment. Segment 0 is the
// normal single-segment case.
func (c Capture) Channel(channel string, segment int) (Waveform, bool) {
	for _, r := range c.Records {
		if r.Channel == channel && r.SegmentIndex == segment {
			return r.Waveform, true
		}
	}
	return Waveform{}, false
}

// Channels returns the distinct channel names in first-seen order.
func (c Capture) Channels() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, r := range c.Records {
		if !seen[r.Channel] {
			seen[r.Channel] = true
			out = append(out, r.Channel)
		}
	}
	return out
}

// Segments returns the sorted distinct segment indices.
func (c Capture) Segments() []int {
	seen := make(map[int]bool)
	out := make([]int, 0)
	for _, r := range c.Records {
		if !seen[r.SegmentIndex] {
			seen[r.SegmentIndex] = true
			out = append(out, r.SegmentIndex)
		}
	}
	// small slice; simple insertion sort avoids another import
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Validate checks duplicate channel/segment pairs and basic waveform validity.
func (c Capture) Validate() error {
	seen := make(map[string]bool)
	for i, r := range c.Records {
		if r.Channel == "" {
			return fmt.Errorf("decode: record %d has an empty channel name", i)
		}
		key := fmt.Sprintf("%s/%d", r.Channel, r.SegmentIndex)
		if seen[key] {
			return fmt.Errorf("decode: duplicate record for channel %q segment %d", r.Channel, r.SegmentIndex)
		}
		seen[key] = true
		if err := r.Waveform.Validate(); err != nil {
			return fmt.Errorf("decode: record %s/%d: %w", r.Channel, r.SegmentIndex, err)
		}
	}
	return nil
}
