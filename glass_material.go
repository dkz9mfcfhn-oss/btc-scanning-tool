package main

import (
	"math"
	"sync"
)

type materialColor [3]float64

func materialRGB(c uintptr) materialColor {
	return materialColor{float64(c & 255), float64(c >> 8 & 255), float64(c >> 16 & 255)}
}
func unit(t float64) float64 { return max(0, min(1, t)) }

func materialMix(a, b materialColor, t float64) materialColor {
	return materialColor{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

type glassTileKey struct {
	Width, Height, X, Y, CanvasWidth, CanvasHeight, Radius int
	Theme                                                  string
	Base                                                   uintptr
	Surface, Shadow                                        bool
}

type materialScene struct {
	background                         materialColor
	width, height, invWidth, invHeight float64
	strength                           float64
	light                              bool
	field                              *materialLightField
}

type materialLight struct {
	x, y, sx, sy float64
	color        materialColor
}

var sceneLights = [...]materialLight{
	{0.04, 0.08, 0.35, 0.36, materialColor{77, 167, 245}},
	{0.69, 0.08, 0.37, 0.31, materialColor{158, 132, 239}},
	{0.98, 0.62, 0.35, 0.44, materialColor{103, 210, 194}},
	{0.30, 0.99, 0.44, 0.31, materialColor{233, 153, 210}},
	{0.32, 0.37, 0.26, 0.18, materialColor{213, 231, 255}},
}

// The ambient light field is smooth, continuous and independent of window
// size. Cache samples in normalized coordinates, including overscan for the
// rounded rim's refraction. Controls still use the same scene as their parent.
const fieldSteps = 144
const fieldSide = fieldSteps + 1
const fieldMargin = 0.25

type materialLightField [fieldSide * fieldSide]materialColor

var materialLightFields [4]struct {
	once   sync.Once
	colors materialLightField
}

func newMaterialScene(k glassTileKey) materialScene {
	theme := normalizedTheme(k.Theme)
	s := materialScene{
		background: materialRGB(paletteFor(theme).background),
		width:      float64(max(1, k.CanvasWidth)),
		height:     float64(max(1, k.CanvasHeight)),
		strength:   0.33,
		light:      lightTheme(theme),
	}
	s.invWidth, s.invHeight = 1/s.width, 1/s.height
	if minimalTheme(theme) {
		s.strength = 0
		return s
	}
	if theme == "glass" {
		s.strength = 0.73
	} else if theme == "light" {
		s.strength = 0.43
	}
	var index int
	for i, name := range themeChoices[:len(materialLightFields)] {
		if name == theme {
			index = i
			break
		}
	}
	field := &materialLightFields[index]
	field.once.Do(func() {
		const step = (1 + 2*fieldMargin) / fieldSteps
		for y := range fieldSide {
			for x := range fieldSide {
				field.colors[y*fieldSide+x] = s.analyticAt(float64(x)*step-fieldMargin, float64(y)*step-fieldMargin)
			}
		}
	})
	s.field = &field.colors
	return s
}

func (s *materialScene) analyticAt(x, y float64) materialColor {
	c := s.background
	for _, g := range sceneLights {
		dx, dy := (x-g.x)/g.sx, (y-g.y)/g.sy
		c = materialMix(c, g.color, math.Exp(-(dx*dx+dy*dy)*1.65)*s.strength)
	}
	return c
}

func (s *materialScene) at(x, y float64) materialColor {
	if s.field == nil {
		return s.background
	}
	const scale = fieldSteps / (1 + 2*fieldMargin)
	x, y = x*s.invWidth, y*s.invHeight
	xg, yg := (x+fieldMargin)*scale, (y+fieldMargin)*scale
	if xg < 0 || yg < 0 || xg >= fieldSteps || yg >= fieldSteps {
		return s.analyticAt(x, y)
	}
	xi, yi := int(xg), int(yg)
	u, v := xg-float64(xi), yg-float64(yi)
	i := yi*fieldSide + xi
	top := materialMix(s.field[i], s.field[i+1], u)
	bottom := materialMix(s.field[i+fieldSide], s.field[i+fieldSide+1], u)
	return materialMix(top, bottom, v)
}

// roundedSurface returns the signed distance and outward normal. The same
// normal determines pixel coverage, lens displacement and incident light.
func roundedSurface(x, y, w, h, r float64) (distance, nx, ny float64) {
	x, y = x-w/2, y-h/2
	qx, qy := math.Abs(x)-(w/2-r), math.Abs(y)-(h/2-r)
	switch {
	case qx > 0 && qy > 0:
		length := math.Hypot(qx, qy)
		return length - r, math.Copysign(qx/length, x), math.Copysign(qy/length, y)
	case qx > qy:
		return qx - r, math.Copysign(1, x), 0
	default:
		return qy - r, 0, math.Copysign(1, y)
	}
}

// renderGlassTile composites a surface over its scene for portable previews.
func renderGlassTile(k glassTileKey) []byte {
	return renderGlassRaster(k, k.Width, k.Height)
}

func glassRasterSize(k glassTileKey) (int, int) {
	// At 50% zoom a maximized 4K window can exceed 7,000 logical units.
	// Keep the soft background and wide panels cheap to create and cache.
	if !k.Surface {
		return min(k.Width, 864), min(k.Height, 576)
	}
	return min(k.Width, 1536), min(k.Height, 1024)
}

func validGlassRaster(k glassTileKey, width, height int) bool {
	return k.Width > 0 && k.Height > 0 && width > 0 && height > 0 && width <= 8192 && height <= 8192
}

func renderGlassRaster(k glassTileKey, width, height int) []byte {
	if !validGlassRaster(k, width, height) {
		return nil
	}
	var data []byte
	if k.Surface {
		data = renderGlassLayer(k, width, height)
	} else {
		data = make([]byte, width*height*4)
	}
	if minimalTheme(k.Theme) && !k.Surface {
		bg := paletteFor(k.Theme).background
		for i := 0; i < len(data); i += 4 {
			data[i], data[i+1], data[i+2], data[i+3] = byte(bg>>16), byte(bg>>8), byte(bg), 255
		}
		return data
	}
	if minimalTheme(k.Theme) {
		return compositeFlatTile(k, data)
	}
	scene := newMaterialScene(k)
	xStep, yStep := float64(k.Width)/float64(width), float64(k.Height)/float64(height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			gx := float64(k.X) + (float64(x)+0.5)*xStep - 0.5
			gy := float64(k.Y) + (float64(y)+0.5)*yStep - 0.5
			c := scene.at(gx, gy)
			i := (y*width + x) * 4
			if !k.Surface {
				data[i], data[i+1], data[i+2], data[i+3] = byte(c[2]+0.5), byte(c[1]+0.5), byte(c[0]+0.5), 255
				continue
			}
			inv := 1 - float64(data[i+3])/255
			for component := 0; component < 3; component++ {
				data[i+component] = byte(min(255, float64(data[i+component])+math.Round(c[2-component]*inv)))
			}
			data[i+3] = 255
		}
	}
	return data
}

type bevelLight struct{ bend, outline, glint, dispersion, inner float64 }

const bevelStepsPerUnit = 16
const bevelDepthLimit = 22

var bevelLights = func() [bevelDepthLimit*bevelStepsPerUnit + 1]bevelLight {
	var values [bevelDepthLimit*bevelStepsPerUnit + 1]bevelLight
	for i := range values {
		depth := float64(i) / bevelStepsPerUnit
		gauss := func(center, width float64) float64 {
			x := (depth - center) / width
			return math.Exp(-x * x)
		}
		values[i] = bevelLight{
			bend:       21*gauss(1.6, 3.4) + 5*math.Exp(-depth/8)*(1-depth/bevelDepthLimit),
			outline:    gauss(0, 1.6),
			glint:      gauss(1.45, 1.25),
			dispersion: gauss(3.6, 2.35),
			inner:      gauss(5.7, 3.6),
		}
	}
	return values
}()

func bevelAt(depth float64) bevelLight {
	if depth >= bevelDepthLimit {
		return bevelLight{}
	}
	sample := depth * bevelStepsPerUnit
	i := int(sample)
	t := sample - float64(i)
	a, b := bevelLights[i], bevelLights[i+1]
	return bevelLight{
		bend:       a.bend + (b.bend-a.bend)*t,
		outline:    a.outline + (b.outline-a.outline)*t,
		glint:      a.glint + (b.glint-a.glint)*t,
		dispersion: a.dispersion + (b.dispersion-a.dispersion)*t,
		inner:      a.inner + (b.inner-a.inner)*t,
	}
}

type glassOptics struct {
	scene                        materialScene
	base                         materialColor
	edge                         materialColor
	tint, gloss, opacity         float64
	bevelScale                   float64
	invWidth, invBevelScale      float64
	radius, width, height, inset float64
}

func newGlassOptics(k glassTileKey) glassOptics {
	s := newMaterialScene(k)
	g := glassOptics{scene: s, base: materialRGB(k.Base), edge: materialColor{10, 20, 46}, tint: 0.66, gloss: 0.70, opacity: 0.91}
	if k.Shadow {
		g.inset = 8
	}
	g.width, g.height = float64(k.Width)-2*g.inset, float64(k.Height)-2*g.inset
	g.radius = max(0, min(float64(k.Radius)/2, min(g.width, g.height)/2))
	// Thicker cards bend the scene across a wider bevel. A toolbar pill has a
	// narrow, brighter rim and a clearer face so it reads as a floating lens.
	g.bevelScale = 0.92 + 0.60*unit((g.height-32)/145)
	g.invWidth, g.invBevelScale = 1/g.width, 1/g.bevelScale
	if s.light {
		g.edge = materialColor{66, 83, 137}
		g.tint, g.gloss, g.opacity = 0.62, 0.78, 0.87
		if normalizedTheme(k.Theme) == "glass" {
			g.tint, g.gloss, g.opacity = 0.44, 0.94, 0.79
			if g.height <= 50 && !k.Shadow {
				g.tint, g.gloss, g.opacity = 0.34, 1.0, 0.77
			}
		}
	}
	low, high := min(g.base[0], g.base[1], g.base[2]), max(g.base[0], g.base[1], g.base[2])
	accented := high-low > 48 && (s.light && low < 100 || !s.light && high > 140)
	if accented {
		// Keep strong action colours vivid while still lighting the rim.
		g.tint, g.gloss, g.opacity = 0.97, 0.45, 0.98
	}
	return g
}

func (g *glassOptics) refract(gx, focusY, x, depth, nx, ny, rowGlow float64) materialColor {
	// Magnify gently through the face, then bend the transmitted colors along
	// the normal. The narrower path for blue leaves a cool fringe on the rim.
	depth *= g.invBevelScale
	horizontal := unit(x * g.invWidth)
	halo := rowGlow * (1 - 0.48*horizontal)
	focusX := gx + min(30, max(-30, (g.width/2-x)*0.086))
	if depth >= bevelDepthLimit {
		c := materialMix(g.scene.at(focusX, focusY), g.base, g.tint)
		return materialMix(c, materialColor{247, 252, 255}, halo)
	}
	bevel := bevelAt(depth)
	pickX, pickY := focusX-nx*bevel.bend*g.bevelScale, focusY-ny*bevel.bend*g.bevelScale
	transmitted := g.scene.at(pickX, pickY)
	if depth < 7 && g.gloss > 0.5 {
		spread := 13 * bevel.dispersion * g.bevelScale
		warmRay := g.scene.at(pickX+nx*spread, pickY+ny*spread)
		coolRay := g.scene.at(pickX-nx*spread, pickY-ny*spread)
		transmitted[0], transmitted[2] = warmRay[0], coolRay[2]
	}
	c := materialMix(transmitted, g.base, g.tint)
	c = materialMix(c, materialColor{247, 252, 255}, halo)
	incident := unit(0.58 - 0.37*nx - 0.53*ny)
	// The shaded outer outline gives the white specular line a dark edge
	// to light against. Opposing bevels catch different colored bounce light.
	shade := g.gloss * ((0.17+0.31*(1-incident))*bevel.outline + (0.07+0.15*(1-incident))*bevel.inner)
	c = materialMix(c, g.edge, shade)
	reflected := materialMix(materialColor{124, 190, 249}, materialColor{193, 170, 249}, unit(0.52+0.36*nx-0.23*ny))
	c = materialMix(c, reflected, g.gloss*(0.18+0.09*(1-incident))*bevel.dispersion)
	specular := g.gloss * (0.28 + 0.72*incident) * bevel.glint * (1 - 0.22*horizontal)
	return materialMix(c, materialColor{254, 254, 255}, specular)
}

// renderGlassLayer builds premultiplied BGRA for native alpha compositing.
// Pixels beyond the antialiased contour or soft shadow have zero alpha and
// never replace the parent's backdrop, even when nested over another tile.
func renderGlassLayer(k glassTileKey, width, height int) []byte {
	if !validGlassRaster(k, width, height) {
		return nil
	}
	data := make([]byte, width*height*4)
	paintGlassLayerPixels(k, width, height, data)
	return data
}

// paintGlassLayerPixels also accepts the native DIB memory directly, avoiding a
// full temporary raster allocation and copy on a cold paint.
func paintGlassLayerPixels(k glassTileKey, width, height int, data []byte) {
	if minimalTheme(k.Theme) {
		paintFlatLayer(k, width, height, data)
		return
	}
	g := newGlassOptics(k)
	if g.width <= 0 || g.height <= 0 {
		clear(data)
		return
	}
	xStep, yStep := float64(k.Width)/float64(width), float64(k.Height)/float64(height)
	shadow := materialColor{41, 60, 96}
	if !g.scene.light {
		shadow = materialColor{1, 8, 24}
	}
	for y := 0; y < height; y++ {
		localY := (float64(y) + 0.5) * yStep
		py := localY - g.inset
		gy := float64(k.Y) + localY - 0.5
		focusY := gy + min(21, max(-21, (g.height/2-py)*0.096))
		vertical := unit(py / g.height)
		rowGlow := g.gloss * 0.20 * math.Exp(-vertical*vertical*16)
		for x := 0; x < width; x++ {
			localX := (float64(x) + 0.5) * xStep
			px := localX - g.inset
			d, nx, ny := roundedSurface(px, py, g.width, g.height, g.radius)
			pixelEdge := yStep
			if ny == 0 {
				pixelEdge = xStep
			} else if nx != 0 {
				pixelEdge = math.Hypot(xStep*nx, yStep*ny)
			}
			pixelEdge = max(0.5, pixelEdge)
			coverage := unit(0.5 - d/pixelEdge)
			face := coverage * g.opacity
			shade := 0.0
			if k.Shadow && coverage < 1 {
				sd, _, _ := roundedSurface(px, py-2.1, g.width, g.height, g.radius)
				fade := unit(1 - max(0, sd)/5.9)
				shade = (1 - coverage) * 0.23 * fade * fade
			}
			alpha := face + shade
			i := (y*width + x) * 4
			if alpha < 0.5/255 {
				clear(data[i : i+4])
				continue
			}
			var c materialColor
			if face > 0 {
				c = g.refract(float64(k.X)+localX-0.5, focusY, px, max(0, -d), nx, ny, rowGlow)
			}
			for component := 0; component < 3; component++ {
				data[i+component] = byte(c[2-component]*face + shadow[2-component]*shade + 0.5)
			}
			data[i+3] = byte(alpha*255 + 0.5)
		}
	}
}

func paintFlatLayer(k glassTileKey, width, height int, data []byte) {
	p := paletteFor(k.Theme)
	face, edge := materialRGB(k.Base), materialRGB(p.border)
	if k.Base == p.accent || k.Base == p.selection {
		edge = face
	}
	var inset float64
	if k.Shadow {
		inset = 8
	}
	w, h := float64(k.Width)-2*inset, float64(k.Height)-2*inset
	if w <= 0 || h <= 0 {
		clear(data)
		return
	}
	radius := min(8, min(w, h)/2, float64(k.Radius)/2)
	sx, sy := float64(k.Width)/float64(width), float64(k.Height)/float64(height)
	px := max(sx, sy, .5)
	for y := range height {
		for x := range width {
			d, _, _ := roundedSurface((float64(x)+.5)*sx-inset, (float64(y)+.5)*sy-inset, w, h, radius)
			a := unit(.5 - d/px)
			c := materialMix(edge, face, unit((-d-.4)/px))
			i := (width*y + x) * 4
			data[i], data[i+1], data[i+2], data[i+3] = byte(c[2]*a+.5), byte(c[1]*a+.5), byte(c[0]*a+.5), byte(255*a+.5)
		}
	}
}

func compositeFlatTile(k glassTileKey, data []byte) []byte {
	bg := paletteFor(k.Theme).background
	for i := 0; i < len(data); i += 4 {
		inv := (255 - float64(data[i+3])) / 255
		data[i] += byte(float64(byte(bg>>16))*inv + .5)
		data[i+1] += byte(float64(byte(bg>>8))*inv + .5)
		data[i+2] += byte(float64(byte(bg))*inv + .5)
		data[i+3] = 255
	}
	return data
}
