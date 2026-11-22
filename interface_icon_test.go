package main

import "testing"

func TestIconLabelKeepsRoomForCaption(t *testing.T) {
	bounds := uiBox{10, 20, 100, 30}
	icon, caption := iconTextPlacement(bounds, 72, 16)
	if icon != (uiBox{13, 27, 16, 16}) || caption.X != 34 || caption.W < 72 || caption.Y != bounds.Y || caption.H != bounds.H {
		t.Fatalf("caption and icon do not fit the same centered group: icon=%+v caption=%+v", icon, caption)
	}
	for _, b := range []uiBox{{10, 20, 92, 30}, {10, 20, 100, 18}} {
		icon, caption = iconTextPlacement(b, 72, 16)
		if icon.W != 0 || caption != b {
			t.Fatalf("constrained control would clip a caption: icon=%+v caption=%+v bounds=%+v", icon, caption, b)
		}
	}
}

func TestInterfaceIconsRenderAtSupportedZoom(t *testing.T) {
	const color = 0x302010
	for kind := interfaceIcon(1); int(kind) < len(interfaceIconPaths); kind++ {
		sizes := []int{8, 11, 16, 24, 32, 48, 64}
		if kind == iconExternal {
			sizes = append(sizes, minInterfaceIconPixels)
		}
		if kind == iconChevron {
			sizes = append(sizes, 5, 6)
		}
		for _, size := range sizes {
			pixels := make([]byte, size*size*4)
			rasterInterfaceIcon(kind, size, color, pixels)
			visible, faded, transparent := 0, 0, 0
			for i := 0; i < len(pixels); i += 4 {
				a := int(pixels[i+3])
				switch {
				case a == 0:
					transparent++
				case a < 255:
					faded++
				}
				if a > 40 {
					visible++
				}
				for channel, value := range [...]int{0x30, 0x20, 0x10} {
					if int(pixels[i+channel]) != (a*value+127)/255 {
						t.Fatalf("icon=%d size=%d: invalid premultiplied BGRA pixel at %d: %v", kind, size, i, pixels[i:i+4])
					}
				}
			}
			if visible == 0 || faded == 0 || transparent == 0 {
				t.Fatalf("icon=%d size=%d: missing visible mark, smooth edge or background (%d, %d, %d pixels)", kind, size, visible, faded, transparent)
			}
		}
	}
}
