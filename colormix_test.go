package csscolorparser

import (
	"math"
	"strings"
	"testing"
)

func approx(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

func testColorApprox(t *testing.T, got, want Color, eps float64) {
	t.Helper()
	if !approx(got.R, want.R, eps) || !approx(got.G, want.G, eps) ||
		!approx(got.B, want.B, eps) || !approx(got.A, want.A, eps) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertFinite(t *testing.T, c Color, msg string) {
	t.Helper()
	if math.IsNaN(c.R) || math.IsNaN(c.G) || math.IsNaN(c.B) || math.IsNaN(c.A) ||
		math.IsInf(c.R, 0) || math.IsInf(c.G, 0) || math.IsInf(c.B, 0) || math.IsInf(c.A, 0) {
		t.Errorf("%s: non-finite result %v", msg, c)
	}
}

func TestColorMixBasic(t *testing.T) {
	red := Color{1, 0, 0, 1}
	blue := Color{0, 0, 1, 1}

	data := []struct {
		s    string
		want Color
	}{
		// default 50/50
		{"color-mix(in srgb, red, blue)", Color{0.5, 0, 0.5, 1}},
		// percentage before or after, or on the other operand (complement)
		{"color-mix(in srgb, red 25%, blue)", Color{0.25, 0, 0.75, 1}},
		{"color-mix(in srgb, 25% red, blue)", Color{0.25, 0, 0.75, 1}},
		{"color-mix(in srgb, red, blue 75%)", Color{0.25, 0, 0.75, 1}},
		{"color-mix(in srgb, red, 75% blue)", Color{0.25, 0, 0.75, 1}},
		// both given, sum 100%
		{"color-mix(in srgb, red 25%, blue 75%)", Color{0.25, 0, 0.75, 1}},
		// sum below 100%: normalized colors, alpha scaled by the sum
		{"color-mix(in srgb, red 20%, blue 30%)", Color{0.4, 0, 0.6, 0.5}},
		// sum above 100%: normalized, alpha stays 1
		{"color-mix(in srgb, red 80%, blue 80%)", Color{0.5, 0, 0.5, 1}},
		// zero / full endpoints
		{"color-mix(in srgb, red 0%, blue 100%)", blue},
		{"color-mix(in srgb, red 100%, blue 0%)", red},
		// both weights zero: equal-weight components, alpha zero
		{"color-mix(in srgb, red 0%, blue 0%)", Color{0.5, 0, 0.5, 0}},
		// transparent operand: premultiplied interpolation
		{"color-mix(in srgb, transparent, red)", Color{1, 0, 0, 0.5}},
		{"color-mix(in srgb, #ff000000, blue)", Color{0, 0, 1, 0.5}},
		{"color-mix(in srgb, transparent, transparent)", Color{0, 0, 0, 0}},
		// retained color of two fully transparent endpoints is averaged, not blackened
		{"color-mix(in srgb, #ff000000, #0000ff00)", Color{0.5, 0, 0.5, 0}},
		// equal alpha on both sides premultiplies and un-premultiplies
		{"color-mix(in srgb, rgba(255, 0, 0, 0.5), rgba(0, 0, 255, 0.5))",
			Color{0.5, 0, 0.5, 0.5}},
		// legacy comma syntax and space syntax stay intact inside the mix
		{"color-mix(in srgb, rgb(255, 0, 0), rgb(0, 0, 255))", Color{0.5, 0, 0.5, 1}},
		{"color-mix(in srgb, rgb(255 0 0), rgb(0 0 255))", Color{0.5, 0, 0.5, 1}},
		// nested mix as operand: 50% of (red+yellow = rgb(1,.5,0)) with blue
		{"color-mix(in srgb, color-mix(in srgb, red, yellow), blue)",
			Color{0.5, 0.25, 0.5, 1}},
		// relative color as operand (hsl(from red 120 s l) is lime)
		{"color-mix(in srgb, hsl(from red 120 s l), black)", Color{0, 0.5, 0, 1}},
		// tabs and newlines are whitespace
		{"color-mix(in\tsrgb,\nred, blue)", Color{0.5, 0, 0.5, 1}},
	}

	for _, d := range data {
		c, err := Parse(d.s)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", d.s, err)
			continue
		}
		assertFinite(t, c, d.s)
		testColorApprox(t, c, d.want, 1e-9)
	}
}

func TestColorMixCaseInsensitive(t *testing.T) {
	c, err := Parse("COLOR-MIX(IN SRGB, RED, BLUE)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{0.5, 0, 0.5, 1}, 1e-9)
}

func TestColorMixAsRelativeSource(t *testing.T) {
	// The mix result feeds an existing relative color.
	c, err := Parse("rgb(from color-mix(in srgb, red, blue) r g b / alpha)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{0.5, 0, 0.5, 1}, 1e-9)

	c, err = Parse("color(from color-mix(in srgb, red, yellow) srgb r g b)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{1, 0.5, 0, 1}, 1e-9)

	// nested mix of a relative color used as a relative source.
	// loose tolerance: the existing relative oklab path has a known
	// white-point residual (oklab(from red l a b) already deviates ~1e-3)
	c, err = Parse("oklab(from color-mix(in oklab, hsl(from blue h s l), red) l a b)")
	if err != nil {
		t.Fatal(err)
	}
	want, err := Parse("color-mix(in oklab, blue, red)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, want, 2e-3)
}

func TestColorMixHuePaths(t *testing.T) {
	// Mixing red (hue 0) and blue (hue 240) in HSL:
	// shorter/decreasing pass through magenta (300),
	// longer/increasing pass through green (120).
	data := []struct {
		s    string
		want Color
	}{
		{"color-mix(in hsl, red, blue)", Color{1, 0, 1, 1}},
		{"color-mix(in hsl shorter hue, red, blue)", Color{1, 0, 1, 1}},
		{"color-mix(in hsl longer hue, red, blue)", Color{0, 1, 0, 1}},
		{"color-mix(in hsl increasing hue, red, blue)", Color{0, 1, 0, 1}},
		{"color-mix(in hsl decreasing hue, red, blue)", Color{1, 0, 1, 1}},
	}
	for _, d := range data {
		c, err := Parse(d.s)
		if err != nil {
			t.Errorf("%q: %v", d.s, err)
			continue
		}
		assertFinite(t, c, d.s)
		testColorApprox(t, c, d.want, 1e-9)
	}

	// numeric achromatic endpoint: its hue is missing, the other hue is used
	c, err := Parse("color-mix(in hsl, hsl(0 0% 50%), red)")
	if err != nil {
		t.Fatal(err)
	}
	assertFinite(t, c, "gray/red hsl")
	testColorApprox(t, c, FromHsl(0, 0.5, 0.5, 1), 1e-9)

	// both achromatic: hue is irrelevant, result is a plain gray
	c, err = Parse("color-mix(in hsl, white, black)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{0.5, 0.5, 0.5, 1}, 1e-9)
}

func TestColorMixPolarZeroWeight(t *testing.T) {
	// A zero-weight achromatic endpoint must not corrupt the kept color.
	c, err := Parse("color-mix(in lch, white 0%, red 100%)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{1, 0, 0, 1}, 1e-6)

	c, err = Parse("color-mix(in oklch, red 0%, white 100%)")
	if err != nil {
		t.Fatal(err)
	}
	// tolerance follows the package's existing oklab white-point constants
	testColorApprox(t, c, Color{1, 1, 1, 1}, 2e-3)

	// zero weights in a polar space: equal-weight components, alpha 0
	c, err = Parse("color-mix(in lch, red 0%, blue 0%)")
	if err != nil {
		t.Fatal(err)
	}
	assertFinite(t, c, "lch zero weights")
	if !approx(c.A, 0, 1e-9) {
		t.Errorf("alpha: got %v, want 0", c.A)
	}
}

func TestColorMixSpaces(t *testing.T) {
	white := Color{1, 1, 1, 1}

	wLin := white.LinearRgb()
	wHsl := white.Hsl()
	bHsl := Color{}.Hsl()
	wHwb := white.Hwb()
	bHwb := Color{}.Hwb()
	wLab := white.Lab()
	wLch := white.Lch()
	wOklab := white.Oklab()
	wOklch := white.Oklch()
	wD65 := white.XyzD65()
	wD50 := white.XyzD50()

	// Expected values are built from the package's own conversions so that
	// these tests cover mixing, not the precision of the color constants.
	data := []struct {
		space string
		want  Color
	}{
		{"srgb", Color{0.5, 0.5, 0.5, 1}},
		{"srgb-linear", FromLinearRGB(wLin[0]/2, wLin[1]/2, wLin[2]/2, 1)},
		{"lab", FromLab(wLab[0]/2, wLab[1]/2, wLab[2]/2, 1)},
		{"oklab", FromOklab(wOklab[0]/2, wOklab[1]/2, wOklab[2]/2, 1)},
		// white is numerically achromatic so its hue is missing
		{"lch", FromLch(wLch[0]/2, wLch[1]/2, 0, 1)},
		{"oklch", FromOklch(wOklch[0]/2, wOklch[1]/2, 0, 1)},
		{"hsl", FromHsl(wHsl[0], (wHsl[1]+bHsl[1])/2, (wHsl[2]+bHsl[2])/2, 1)},
		{"hwb", FromHwb(wHwb[0], (wHwb[1]+bHwb[1])/2, (wHwb[2]+bHwb[2])/2, 1)},
		{"xyz", FromXyzD65(wD65[0]/2, wD65[1]/2, wD65[2]/2, 1)},
		{"xyz-d65", FromXyzD65(wD65[0]/2, wD65[1]/2, wD65[2]/2, 1)},
		{"xyz-d50", FromXyzD50(wD50[0]/2, wD50[1]/2, wD50[2]/2, 1)},
	}

	for _, d := range data {
		s := "color-mix(in " + d.space + ", white, black)"
		c, err := Parse(s)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		assertFinite(t, c, s)
		testColorApprox(t, c, d.want, 1e-9)
	}

	// "xyz" is an alias for "xyz-d65"
	a, _ := Parse("color-mix(in xyz, red, blue)")
	b, _ := Parse("color-mix(in xyz-d65, red, blue)")
	testColorApprox(t, a, b, 1e-12)

	// lch mix of white and red uses red's hue (white's is missing)
	redLch := Color{1, 0, 0, 1}.Lch()
	c, err := Parse("color-mix(in lch, white, red)")
	if err != nil {
		t.Fatal(err)
	}
	want := FromLch((wLch[0]+redLch[0])/2, (wLch[1]+redLch[1])/2, redLch[2], 1)
	assertFinite(t, c, "lch white red")
	testColorApprox(t, c, want, 1e-9)
}

func TestColorMixNoNaN(t *testing.T) {
	spaces := []string{
		"srgb", "srgb-linear", "hsl", "hwb", "lab", "lch",
		"oklab", "oklch", "xyz", "xyz-d65", "xyz-d50",
	}
	for _, space := range spaces {
		for _, args := range [][2]string{
			{"red 0%", "blue 0%"},
			{"transparent", "transparent"},
			{"#ff000000", "#0000ff00"},
			{"white 0%", "black 100%"},
			{"white 100%", "black 0%"},
		} {
			s := "color-mix(in " + space + ", " + args[0] + ", " + args[1] + ")"
			c, err := Parse(s)
			if err != nil {
				t.Errorf("%q: %v", s, err)
				continue
			}
			assertFinite(t, c, s)
		}
	}
}

func TestInterpolateHue(t *testing.T) {
	data := []struct {
		h1, h2, t float64
		method    string
		want      float64
	}{
		{350, 10, 0.5, "shorter", 0},
		{350, 10, 0.5, "longer", 180},
		{350, 10, 0.5, "increasing", 0},
		{350, 10, 0.5, "decreasing", 180},
		{10, 350, 0.5, "increasing", 180},
		{10, 350, 0.5, "decreasing", 0},
		{0, 240, 0.5, "shorter", 300},
		{0, 240, 0.5, "longer", 120},
		{10, 350, 0, "shorter", 10},
		{10, 350, 1, "shorter", 350},
	}
	for _, d := range data {
		got := interpolateHue(d.h1, d.h2, d.t, d.method)
		if !approx(got, d.want, 1e-9) {
			t.Errorf("interpolateHue(%v,%v,%v,%s) = %v, want %v",
				d.h1, d.h2, d.t, d.method, got, d.want)
		}
	}

	// missing hues
	if !math.IsNaN(interpolateHue(math.NaN(), math.NaN(), 0.5, "shorter")) {
		t.Error("both missing should stay NaN")
	}
	if got := interpolateHue(math.NaN(), 240, 0.3, "shorter"); !approx(got, 240, 1e-9) {
		t.Errorf("missing first = %v, want 240", got)
	}
	if got := interpolateHue(120, math.NaN(), 0.3, "longer"); !approx(got, 120, 1e-9) {
		t.Errorf("missing second = %v, want 120", got)
	}
}

func TestColorMixInvalid(t *testing.T) {
	data := []string{
		"color-mix()",
		"color-mix(in srgb, red)",
		"color-mix(in srgb, red, blue, green)",
		"color-mix(in srgb, red, blue,)",
		"color-mix(in srgb,)",
		"color-mix(in srgb,, blue)",
		"color-mix(in srgb, , blue)",
		"color-mix(in srgb, red, )",
		"color-mix(in foo, red, blue)",
		"color-mix(in display-p3, red, blue)",
		"color-mix(in srgb shorter hue, red, blue)",
		"color-mix(in srgb hue, red, blue)",
		"color-mix(in srgb, red, blue) hue",
		"color-mix(in hsl foo hue, red, blue)",
		"color-mix(in hsl shorter, red, blue)",
		"color-mix(in hsl hue, red, blue)",
		"color-mix(in srgb red blue)",
		"color-mix(insrgb, red, blue)",
		"color-mix(in,srgb,red,blue)",
		"color-mix(in srgb, red blue)",
		"color-mix(in srgb, red 50, blue)",
		"color-mix(in srgb, red 50 %%, blue)",
		"color-mix(in srgb, red %, blue)",
		"color-mix(in srgb, red 101%, blue)",
		"color-mix(in srgb, red -5%, blue)",
		"color-mix(in srgb, red 20% 30%, blue)",
		"color-mix(in srgb, 20% red 30%, blue)",
		"color-mix(in srgb, bloodred, blue)",
		"color-mix(in srgb, red, cmyk(1, 0, 0))",
		"color-mix(in srgb, red, blue",
		"color-mix(in srgb, red, blue))",
		"color-mix(in srgb, red, blue) extra",
		"color-mix",
		"color-mix(",
	}
	for _, s := range data {
		c, err := Parse(s)
		if err == nil {
			t.Errorf("%q: expected error, got %v", s, c)
			continue
		}
		if c != black {
			t.Errorf("%q: error result should be black, got %v", s, c)
		}
	}
}

func TestColorMixInvalidNoPanic(t *testing.T) {
	// Random malformed prefixes must return errors, never panic.
	for _, s := range []string{
		"color-mix(in srgb, red, blue) ",
		"color-mix( in srgb , red , blue )",
	} {
		if _, err := Parse(s); err != nil {
			t.Errorf("%q: unexpected error: %v", s, err)
		}
	}
}

func TestColorMixRecursionLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("color-mix(in srgb, ")
	for i := 0; i < 40; i++ {
		b.WriteString("color-mix(in srgb, ")
	}
	b.WriteString("red")
	for i := 0; i < 40; i++ {
		b.WriteString(", blue)")
	}
	b.WriteString(", blue)")

	if _, err := Parse(b.String()); err == nil {
		t.Error("deeply nested color-mix should be rejected")
	}
}

func TestColorMixStateIsolation(t *testing.T) {
	if _, err := Parse("color-mix(in srgb, red, blue, green)"); err == nil {
		t.Fatal("expected error")
	}
	// an unrelated valid parse after the error must not inherit anything
	c, err := Parse("red")
	if err != nil {
		t.Fatal(err)
	}
	if c != (Color{1, 0, 0, 1}) {
		t.Errorf("got %v", c)
	}

	// two independent deep parses produce the same result
	a, err1 := Parse("color-mix(in lch longer hue, teal, olive)")
	b, err2 := Parse("color-mix(in lch longer hue, teal, olive)")
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if a != b {
		t.Errorf("independent calls differ: %v vs %v", a, b)
	}
}

func TestColorMixUnmarshalText(t *testing.T) {
	var c Color
	if err := c.UnmarshalText([]byte("color-mix(in srgb, red 20%, blue 30%)")); err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, c, Color{0.4, 0, 0.6, 0.5}, 1e-9)

	if err := c.UnmarshalText([]byte("color-mix(in bogus, red, blue)")); err == nil {
		t.Error("expected error")
	}
}

func TestColorMixSpecVectors(t *testing.T) {
	// CSS Color 5 worked example: 40% peru + 60% palegoldenrod in lch is
	// lch(79.7256% 40.448 84.771).
	c, err := Parse("color-mix(in lch, lch(62.253% 54.011 63.677) 40%, lch(91.374% 31.406 98.834))")
	if err != nil {
		t.Fatal(err)
	}
	v := c.Lch()
	if !approx(v[0], 79.7256, 1e-3) || !approx(v[1], 40.448, 1e-3) ||
		!approx(v[2]*180/math.Pi, 84.771, 1e-3) {
		t.Errorf("lch(%.4f %.4f %.4f)", v[0], v[1], v[2]*180/math.Pi)
	}

	// Sum below 100% keeps the normalized components, scales alpha down.
	full, err := Parse("color-mix(in lch, purple 50%, plum 50%)")
	if err != nil {
		t.Fatal(err)
	}
	partial, err := Parse("color-mix(in lch, purple 30%, plum 30%)")
	if err != nil {
		t.Fatal(err)
	}
	testColorApprox(t, partial, Color{full.R, full.G, full.B, 0.6}, 1e-9)

	// hue methods end-to-end in lch: resulting hue follows the selected arc
	redH := Color{1, 0, 0, 1}.Lch()[2] * 180 / math.Pi
	blueH := Color{0, 0, 1, 1}.Lch()[2] * 180 / math.Pi
	expectedHue := map[string]float64{
		"shorter":    interpolateHue(redH, blueH, 0.5, "shorter"),
		"longer":     interpolateHue(redH, blueH, 0.5, "longer"),
		"increasing": interpolateHue(redH, blueH, 0.5, "increasing"),
		"decreasing": interpolateHue(redH, blueH, 0.5, "decreasing"),
	}
	for _, method := range []string{"shorter", "longer", "increasing", "decreasing"} {
		s := "color-mix(in lch " + method + " hue, red, blue)"
		m, err := Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		assertFinite(t, m, s)
		gotH := m.Lch()[2] * 180 / math.Pi
		if !approx(gotH, expectedHue[method], 0.05) {
			t.Errorf("%s: hue %.3f, want %.3f", method, gotH, expectedHue[method])
		}
	}
}

func TestColorMixParseColors(t *testing.T) {
	s := "color-mix(in srgb, red, blue), color-mix(in bogus, red, blue), #00ff00, color-mix(in hsl longer hue, red, blue)"
	var results []Color
	var errs int
	for c, err := range ParseColors(s) {
		if err != nil {
			errs++
			continue
		}
		results = append(results, c)
	}
	if errs != 1 {
		t.Errorf("errors: got %d, want 1", errs)
	}
	if len(results) != 3 {
		t.Fatalf("results: got %d, want 3", len(results))
	}
	testColorApprox(t, results[0], Color{0.5, 0, 0.5, 1}, 1e-9)
	testColorApprox(t, results[1], Color{0, 1, 0, 1}, 1e-9)
	testColorApprox(t, results[2], Color{0, 1, 0, 1}, 1e-9)
}
