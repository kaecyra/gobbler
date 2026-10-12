package ingest

import "math"

// score sets Overall and BelowThreshold. structure is the confidence in the
// shape of the draft (are the name, ingredients and steps there, was the text
// split on headings); the lines then scale it by the mean of their confidences
// and by their worst, averaged, so one unreadable line among many lowers the
// score without a single bad line deciding everything. A draft with no lines
// has nothing to scale by; structure already carries that penalty.
func (d *Draft) score(structure float64) {
	var sum, worst float64
	var n int
	worst = 1
	for _, c := range d.Components {
		for _, l := range c.Lines {
			sum += l.Confidence
			worst = math.Min(worst, l.Confidence)
			n++
		}
	}
	lineFactor := 1.0
	if n > 0 {
		lineFactor = (sum/float64(n) + worst) / 2
	}
	d.Overall = round2(math.Max(structure, 0) * lineFactor)
	d.BelowThreshold = d.Overall < d.Threshold
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
