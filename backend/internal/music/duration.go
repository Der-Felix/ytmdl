package music

// RecordingDurationToleranceMS allows ordinary metadata rounding and short
// intros, but never a complete movement in place of one of its sections.
const RecordingDurationToleranceMS = 15000

// CompatibleDuration compares known runtimes. Unknown metadata alone does not
// reject a search candidate; the downloaded audio must still be verified.
func CompatibleDuration(expectedMS, measuredMS int) bool {
	if expectedMS <= 0 || measuredMS <= 0 {
		return true
	}
	diff := int64(expectedMS) - int64(measuredMS)
	return diff >= -RecordingDurationToleranceMS && diff <= RecordingDurationToleranceMS
}
