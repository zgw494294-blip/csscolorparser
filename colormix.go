package csscolorparser

import (
	"math"
	"strings"
)

// CSS Color 5 color-mix():
//
//	color-mix(in <space> [<hue-method> hue]?, <color> [<pct>]?, <color> [<pct>]?)
//
// Only the explicit two-color form is supported here.

// Numeric thresholds below which a polar color is considered achromatic, so
// its hue is treated as missing during interpolation. The LCH/Oklch limits are
// slightly wider than the residual chroma that neutral sRGB colors acquire
// when converted with this package's color-space constants.
const (
	mixEpsHslS   = 1e-7
	mixEpsHwbWB  = 1e-7
	mixEpsLchC   = 0.02
	mixEpsOklchC = 0.001
)

// color-mix interpolation spaces.
var (
	mixRectangularSpaces = map[string]bool{
		"srgb":        true,
		"srgb-linear": true,
		"lab":         true,
		"oklab":       true,
		"xyz":         true,
		"xyz-d50":     true,
		"xyz-d65":     true,
	}
	mixPolarSpaces = map[string]bool{
		"hsl":   true,
		"hwb":   true,
		"lch":   true,
		"oklch": true,
	}
	mixHueMethods = map[string]bool{
		"shorter":    true,
		"longer":     true,
		"increasing": true,
		"decreasing": true,
	}
)

func isValidMixSpace(space string) bool {
	return mixRectangularSpaces[space] || mixPolarSpaces[space]
}

// trimMixSpace trims the same ASCII whitespace characters that the parameter
// scanner treats as whitespace.
func trimMixSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return r < 128 && isSpace(byte(r))
	})
}

// mixHueIndex returns the index of the hue component for polar color spaces.
func mixHueIndex(space string) int {
	if space == "lch" || space == "oklch" {
		return 2
	}
	return 0 // hsl, hwb
}

// parseColorMix parses the explicit two-color form of color-mix().
// s has already been lowercased and is known to start with "color-mix("
// and end with ")".
func parseColorMix(s string, depth int) (Color, error) {
	const prefix = "color-mix("

	inner := s[len(prefix) : len(s)-1]
	pp := newParamParser(inner)
	pp.space()

	// "in" keyword

	if v, ok := pp.value(); !ok || v != "in" {
		return black, ErrInvalidColorMix
	}
	if !pp.space() {
		return black, ErrInvalidColorMix
	}

	// Interpolation color space

	v, ok := pp.value()
	if !ok {
		return black, ErrInvalidColorMix
	}
	space := v
	if !isValidMixSpace(space) {
		return black, ErrInvalidColorMix
	}

	// Optional hue interpolation method (polar spaces only)

	hueMethod := "shorter"
	pp.space()
	if b, has := pp.peekByte(); has && b != ',' {
		m, okm := pp.value()
		if !okm || !mixHueMethods[m] {
			return black, ErrInvalidColorMix
		}
		if !pp.space() {
			return black, ErrInvalidColorMix
		}
		h, okh := pp.value()
		if !okh || h != "hue" {
			return black, ErrInvalidColorMix
		}
		if !mixPolarSpaces[space] {
			return black, ErrInvalidColorMix
		}
		hueMethod = m
		pp.space()
	}

	// Exactly two color arguments, separated by a top-level comma.

	if !pp.separator(",") {
		return black, ErrInvalidColorMix
	}

	seg1 := pp.segment()
	if !pp.separator(",") {
		return black, ErrInvalidColorMix
	}
	seg2 := pp.segment()
	pp.space()
	if !pp.isEnd() {
		return black, ErrInvalidColorMix
	}

	c1, p1, err1 := parseMixOperand(seg1, depth+1)
	c2, p2, err2 := parseMixOperand(seg2, depth+1)
	if err1 != nil || err2 != nil {
		return black, ErrInvalidColorMix
	}

	return colorMix(c1, c2, space, hueMethod, p1, p2)
}

// parseMixOperand parses one color-mix() argument: a full color value with an
// optional percentage placed either before or after it. The returned
// percentage is nil when omitted. The color may be any absolute color, a
// relative color, or a nested color-mix().
func parseMixOperand(seg string, depth int) (Color, *float64, error) {
	seg = trimMixSpace(seg)
	if seg == "" {
		return black, nil, ErrInvalidColorMix
	}

	// Find the first and last whitespace outside parentheses. A color value
	// never contains top-level whitespace: multi-token colors are all wrapped
	// in parentheses, so such whitespace can only separate the percentage.

	firstSpace, lastSpace := -1, -1
	d := 0
	for i := 0; i < len(seg); i++ {
		switch seg[i] {
		case '(':
			d++
		case ')':
			if d > 0 {
				d--
			}
		default:
			if d == 0 && isSpace(seg[i]) {
				if firstSpace < 0 {
					firstSpace = i
				}
				lastSpace = i
			}
		}
	}

	var weight *float64
	colorStr := seg

	if firstSpace >= 0 {
		head := trimMixSpace(seg[:firstSpace])
		tail := trimMixSpace(seg[lastSpace+1:])

		hv, headPct := parseMixWeight(head)
		tv, tailPct := parseMixWeight(tail)

		switch {
		case headPct && tailPct:
			// Two percentages around one color.
			return black, nil, ErrInvalidColorMix
		case headPct:
			w := hv
			weight = &w
			colorStr = trimMixSpace(seg[firstSpace+1:])
		case tailPct:
			w := tv
			weight = &w
			colorStr = trimMixSpace(seg[:lastSpace])
		default:
			// Stray top-level tokens that are neither a color nor a percentage.
			return black, nil, ErrInvalidColorMix
		}

		if colorStr == "" {
			return black, nil, ErrInvalidColorMix
		}
	}

	c, err := parseAll(colorStr, depth)
	if err != nil {
		return black, nil, err
	}
	return c, weight, nil
}

// parseMixWeight parses a single color-mix() percentage such as "42%".
// The value must be finite and in the range 0..100; it is returned in 0..1.
func parseMixWeight(tok string) (float64, bool) {
	num, ok := strings.CutSuffix(tok, "%")
	if !ok || num == "" {
		return 0, false
	}
	v, ok := parseNumber(num)
	if !ok || !isFinite(v) || v < 0 || v > 100 {
		return 0, false
	}
	return v / 100, true
}

// colorMix performs the numeric mixing of two colors in the given
// interpolation space, following CSS Color 5 percentage normalization and
// CSS Color 4 premultiplied-alpha interpolation.
func colorMix(c1, c2 Color, space, hueMethod string, p1, p2 *float64) (Color, error) {
	// Resolve omitted percentages (CSS Color 5, Percentage Normalization).

	var v1, v2 float64
	switch {
	case p1 == nil && p2 == nil:
		v1, v2 = 0.5, 0.5
	case p1 == nil:
		v1, v2 = 1-*p2, *p2
	case p2 == nil:
		v1, v2 = *p1, 1-*p1
	default:
		v1, v2 = *p1, *p2
	}

	// A positive total is normalized to 1. When the total is below 100%, the
	// result alpha is additionally scaled by it; above 100% opacity is not
	// raised. Both weights zero still yields a valid equal-weight color whose
	// alpha is multiplied by zero.

	sum := v1 + v2
	alphaMult := math.Min(sum, 1)

	var w1, w2 float64
	if sum > 0 {
		w1, w2 = v1/sum, v2/sum
	} else {
		w1, w2 = 0.5, 0.5
	}

	k1 := mixToCoords(c1, space)
	k2 := mixToCoords(c2, space)
	a1 := clamp(c1.A)
	a2 := clamp(c2.A)

	alpha := w1*a1 + w2*a2

	var out [3]float64
	hi := -1
	if mixPolarSpaces[space] {
		hi = mixHueIndex(space)
	}

	for i := 0; i < 3; i++ {
		if i == hi {
			continue // hue is interpolated separately and is never premultiplied
		}
		prem := w1*a1*k1[i] + w2*a2*k2[i]
		if alpha > 0 {
			out[i] = prem / alpha
		} else {
			// Both endpoints are fully transparent: unpremultiplying is
			// undefined, so average the retained color components directly.
			// This keeps the numeric meaning of the endpoints and avoids NaN.
			out[i] = w1*k1[i] + w2*k2[i]
		}
	}

	if hi >= 0 {
		out[hi] = interpolateHue(k1[hi], k2[hi], w2, hueMethod)
	}

	return mixFromCoords(out, space, clamp(alpha*alphaMult)), nil
}

// mixToCoords converts a Color to interpolation coordinates in the given
// space. In polar spaces the hue component (in degrees) is NaN for
// numerically achromatic colors, so it is treated as missing.
func mixToCoords(c Color, space string) [3]float64 {
	switch space {
	case "srgb":
		return [3]float64{c.R, c.G, c.B}
	case "srgb-linear":
		v := c.LinearRgb()
		return [3]float64{v[0], v[1], v[2]}
	case "hsl":
		v := c.Hsl()
		h := v[0]
		if v[1] <= mixEpsHslS {
			h = math.NaN()
		}
		return [3]float64{h, v[1], v[2]}
	case "hwb":
		v := c.Hwb()
		h := v[0]
		if v[1]+v[2] >= 1-mixEpsHwbWB {
			h = math.NaN()
		}
		return [3]float64{h, v[1], v[2]}
	case "lab":
		v := c.Lab()
		return [3]float64{v[0], v[1], v[2]}
	case "lch":
		v := c.Lch() // hue in radians
		h := v[2] * 180 / math.Pi
		if v[1] <= mixEpsLchC {
			h = math.NaN()
		}
		return [3]float64{v[0], v[1], h}
	case "oklab":
		v := c.Oklab()
		return [3]float64{v[0], v[1], v[2]}
	case "oklch":
		v := c.Oklch() // hue in radians
		h := v[2] * 180 / math.Pi
		if v[1] <= mixEpsOklchC {
			h = math.NaN()
		}
		return [3]float64{v[0], v[1], h}
	case "xyz", "xyz-d65":
		v := c.XyzD65()
		return [3]float64{v[0], v[1], v[2]}
	case "xyz-d50":
		v := c.XyzD50()
		return [3]float64{v[0], v[1], v[2]}
	}
	return [3]float64{}
}

// mixFromCoords builds a Color from interpolation coordinates. A missing
// (NaN) hue is replaced with zero, which is immaterial for achromatic
// results and prevents NaN from propagating into trigonometry.
func mixFromCoords(v [3]float64, space string, alpha float64) Color {
	switch space {
	case "srgb":
		return Color{v[0], v[1], v[2], alpha}
	case "srgb-linear":
		return FromLinearRGB(v[0], v[1], v[2], alpha)
	case "hsl":
		h := v[0]
		if math.IsNaN(h) {
			h = 0
		}
		return FromHsl(h, v[1], v[2], alpha)
	case "hwb":
		h := v[0]
		if math.IsNaN(h) {
			h = 0
		}
		return FromHwb(h, v[1], v[2], alpha)
	case "lab":
		return FromLab(math.Max(v[0], 0), v[1], v[2], alpha)
	case "lch":
		h := v[2]
		if math.IsNaN(h) {
			h = 0
		}
		return FromLch(math.Max(v[0], 0), math.Max(v[1], 0), h*math.Pi/180, alpha)
	case "oklab":
		return FromOklab(math.Max(v[0], 0), v[1], v[2], alpha)
	case "oklch":
		h := v[2]
		if math.IsNaN(h) {
			h = 0
		}
		return FromOklch(math.Max(v[0], 0), math.Max(v[1], 0), h*math.Pi/180, alpha)
	case "xyz", "xyz-d65":
		return FromXyzD65(v[0], v[1], v[2], alpha)
	case "xyz-d50":
		return FromXyzD50(v[0], v[1], v[2], alpha)
	}
	return black
}

// interpolateHue interpolates between two hue angles (degrees) at progress t,
// where t=0 yields h1 and t=1 yields h2. A NaN hue means "missing": if only
// one hue is missing the other is used; if both are missing the result is
// missing.
func interpolateHue(h1, h2, t float64, method string) float64 {
	missing1 := math.IsNaN(h1)
	missing2 := math.IsNaN(h2)

	switch {
	case missing1 && missing2:
		return math.NaN()
	case missing1:
		return normalizeAngle(h2)
	case missing2:
		return normalizeAngle(h1)
	}

	a := normalizeAngle(h1)
	b := normalizeAngle(h2)

	switch method {
	case "longer":
		d := modulo(b-a, 360)
		if d < 180 {
			d += 360
		}
		return normalizeAngle(a + d*t)
	case "increasing":
		d := modulo(b-a, 360)
		return normalizeAngle(a + d*t)
	case "decreasing":
		d := modulo(a-b, 360)
		return normalizeAngle(a - d*t)
	default: // shorter
		d := modulo(b-a, 360)
		if d > 180 {
			d -= 360
		}
		return normalizeAngle(a + d*t)
	}
}
