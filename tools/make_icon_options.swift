// Run on macOS with: swift tools/make_icon_options.swift <output-directory> [--round2]
// Creates PNG/ICO pairs and a selection image for the requested set.
import AppKit
import CoreGraphics
import CoreText
import Foundation

let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!

func color(_ rgb: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(srgbRed: CGFloat((rgb >> 16) & 255) / 255,
            green: CGFloat((rgb >> 8) & 255) / 255,
            blue: CGFloat(rgb & 255) / 255, alpha: alpha)
}

func box(_ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat, _ r: CGFloat) -> CGPath {
    CGPath(roundedRect: CGRect(x: x, y: y, width: w, height: h), cornerWidth: r, cornerHeight: r, transform: nil)
}

func fill(_ context: CGContext, _ path: CGPath, _ color: CGColor) {
    context.addPath(path)
    context.setFillColor(color)
    context.fillPath()
}

func outline(_ context: CGContext, _ path: CGPath, _ color: CGColor, _ width: CGFloat) {
    context.addPath(path)
    context.setStrokeColor(color)
    context.setLineWidth(width)
    context.strokePath()
}

func gradient(_ context: CGContext, in path: CGPath, _ stops: [CGColor], from start: CGPoint, to end: CGPoint) {
    let gradient = CGGradient(colorsSpace: colorSpace, colors: stops as CFArray, locations: nil)!
    context.saveGState()
    context.addPath(path)
    context.clip()
    context.drawLinearGradient(gradient, start: start, end: end, options: [.drawsBeforeStartLocation, .drawsAfterEndLocation])
    context.restoreGState()
}

func line(_ context: CGContext, _ points: [(CGFloat, CGFloat)], _ ink: CGColor, width: CGFloat) {
    context.beginPath()
    context.addLines(between: points.map { CGPoint(x: $0.0, y: $0.1) })
    context.setStrokeColor(ink)
    context.setLineWidth(width)
    context.setLineCap(.round)
    context.setLineJoin(.round)
    context.strokePath()
}

func disk(_ x: CGFloat, _ y: CGFloat, _ radius: CGFloat) -> CGPath {
    CGPath(ellipseIn: CGRect(x: x - radius, y: y - radius, width: 2 * radius, height: 2 * radius), transform: nil)
}

func polygon(_ points: [(CGFloat, CGFloat)]) -> CGPath {
    let path = CGMutablePath()
    path.addLines(between: points.map { CGPoint(x: $0.0, y: $0.1) })
    path.closeSubpath()
    return path
}

struct IconDesign {
    let file: String
    let name: String
    let subtitle: String
    let shades: [UInt32]
    let accent: UInt32
}

let designs = [
    IconDesign(file: "01-flare", name: "烈焰芯片", subtitle: "亮橙红 · 芯片与算力脉冲", shades: [0xFFA428, 0xF35613, 0xC21F2F], accent: 0xBF3A22),
    IconDesign(file: "02-spectrum", name: "极光雷达", subtitle: "蓝紫渐变 · 设备监测雷达", shades: [0x31AFFF, 0x5546F4, 0x9B27DF], accent: 0x5A41B9),
    IconDesign(file: "03-volt", name: "电光机阵", subtitle: "荧光绿 · 三层矿机阵列", shades: [0xD2FC39, 0x66DF26, 0x00A074], accent: 0x1C774C),
    IconDesign(file: "04-goldscope", name: "金色取景", subtitle: "蜂蜜金 · 扫描框与硬币", shades: [0xFFF084, 0xFDB423, 0xEE6919], accent: 0x87551A),
    IconDesign(file: "05-pinkcrystal", name: "桃红晶体", subtitle: "亮桃红 · 折面晶体", shades: [0xFFB6E7, 0xF849AD, 0xAA176D], accent: 0xA92473),
    IconDesign(file: "06-cyandetect", name: "青光探针", subtitle: "蓝绿色 · 搜索与波形", shades: [0x83FEF4, 0x21DAC7, 0x008E9C], accent: 0x08786D),
    IconDesign(file: "07-prismgrid", name: "彩紫网格", subtitle: "亮紫色 · 多点扫描阵列", shades: [0xDEB4FF, 0x9E55FF, 0x5724BB], accent: 0x633CB7),
    IconDesign(file: "08-bluebeacon", name: "钴蓝信标", subtitle: "宝石蓝 · 扫描信号塔", shades: [0x83BBFF, 0x245CFF, 0x192483], accent: 0x2950B5),
    IconDesign(file: "09-coralbolt", name: "珊瑚闪电", subtitle: "珊瑚红 · 快速扫描", shades: [0xFFB099, 0xF75061, 0xD32961], accent: 0xAD3453),
]

func drawIcon(_ ctx: CGContext, index: Int, frame: CGRect) {
    ctx.saveGState()
    defer { ctx.restoreGState() }
    ctx.translateBy(x: frame.minX, y: frame.minY)
    ctx.scaleBy(x: frame.width / 128, y: frame.height / 128)
    let tile: CGPath
    switch index {
    case 3:
        tile = polygon([(64, 125), (116, 95), (116, 33), (64, 3), (12, 33), (12, 95)])
    case 6:
        tile = polygon([(63, 124), (122, 65), (63, 4), (5, 65)])
    case 7:
        tile = disk(64, 64, 60)
    default:
        tile = box(4, 4, 120, 120, 29)
    }
    let design = designs[index]
    gradient(ctx, in: tile, design.shades.map { color($0) }, from: CGPoint(x: 12, y: 121), to: CGPoint(x: 115, y: 6))
    if index == 3 || index == 6 || index == 7 {
        outline(ctx, tile, color(0xFFFFFF, 0.38), 1.9)
    } else {
        outline(ctx, box(5, 5, 118, 118, 28), color(0xFFFFFF, 0.30), 1.1)
        gradient(ctx, in: box(8, 8, 112, 112, 26), [color(0xFFFFFF, 0.15), color(0xFFFFFF, 0)], from: CGPoint(x: 62, y: 116), to: CGPoint(x: 64, y: 68))
    }

    switch index {
    case 0:
        for offset in [CGFloat(47), 64, 81] {
            line(ctx, [(offset, 27), (offset, 101)], color(0xFFF2CE), width: 5.8)
            line(ctx, [(27, offset), (101, offset)], color(0xFFF2CE), width: 5.8)
        }
        fill(ctx, box(31, 29, 66, 66, 15), color(0x641530, 0.26))
        gradient(ctx, in: box(32, 32, 64, 64, 14), [color(0x571526), color(0x922336)], from: CGPoint(x: 32, y: 34), to: CGPoint(x: 95, y: 97))
        outline(ctx, box(32, 32, 64, 64, 14), color(0xFFF2CE), 4.5)
        line(ctx, [(43, 59), (54, 59), (60, 72), (68, 52), (75, 65), (86, 65)], color(0xFFFCF2), width: 5.9)
    case 1:
        fill(ctx, disk(63, 61, 42), color(0x242364, 0.30))
        fill(ctx, disk(64, 65, 39), color(0x232168, 0.67))
        outline(ctx, disk(64, 65, 36), color(0xA8F1FF), 4.5)
        outline(ctx, disk(64, 65, 22), color(0xA8F1FF, 0.58), 3)
        outline(ctx, disk(64, 65, 10), color(0xA8F1FF, 0.43), 2.4)
        line(ctx, [(64, 65), (86, 87)], color(0xFCFFFF), width: 6.6)
        fill(ctx, disk(64, 65, 5.7), color(0xF5FFFF))
        fill(ctx, disk(84, 44, 5.6), color(0xFBC7FF))
        fill(ctx, disk(86, 87, 6.1), color(0xF8FFFF))
    case 2:
        fill(ctx, box(25, 27, 78, 70, 17), color(0x003B49, 0.28))
        for y in [CGFloat(75), 53, 31] {
            gradient(ctx, in: box(26, y, 76, 19, 6), [color(0x19364A), color(0x092C35)], from: CGPoint(x: 29, y: y + 19), to: CGPoint(x: 100, y: y))
            outline(ctx, box(26.6, y + 0.6, 74.8, 17.8, 5.5), color(0xD5FFE5, 0.49), 1.2)
            fill(ctx, box(37, y + 7, 31, 4.6, 2.3), color(0xC5FFF1))
            fill(ctx, disk(86, y + 9.5, 3.7), color(0xDDFB5A))
        }
    case 3:
        // Four finder corners frame the high-contrast coin.
        for corner in [[(30.0, 79.0), (30.0, 98.0), (49.0, 98.0)],
                       [(79.0, 98.0), (98.0, 98.0), (98.0, 79.0)],
                       [(30.0, 49.0), (30.0, 30.0), (49.0, 30.0)],
                       [(79.0, 30.0), (98.0, 30.0), (98.0, 49.0)]] {
            line(ctx, corner.map { (CGFloat($0.0), CGFloat($0.1)) }, color(0x623019), width: 6.5)
        }
        fill(ctx, disk(64, 63, 23), color(0x753521))
        outline(ctx, disk(64, 63, 22), color(0xFFF4C4), 4)
        let coin = CGMutablePath()
        coin.move(to: CGPoint(x: 58, y: 51))
        coin.addLine(to: CGPoint(x: 58, y: 76))
        coin.addLine(to: CGPoint(x: 66, y: 76))
        coin.addQuadCurve(to: CGPoint(x: 66, y: 64), control: CGPoint(x: 80, y: 70))
        coin.addQuadCurve(to: CGPoint(x: 66, y: 51), control: CGPoint(x: 82, y: 57))
        coin.addLine(to: CGPoint(x: 58, y: 51))
        outline(ctx, coin, color(0xFFF6D7), 4)
        line(ctx, [(58, 64), (66, 64)], color(0xFFF6D7), width: 4)
    case 4:
        // The diamond reads at 16 px because it is constructed from broad faces.
        let gem = [(23.0, 73.0), (42.0, 95.0), (86.0, 95.0), (105.0, 73.0), (64.0, 24.0)].map { (CGFloat($0.0), CGFloat($0.1)) }
        fill(ctx, polygon([(23, 68), (42, 90), (86, 90), (105, 68), (64, 19)]), color(0x83145C, 0.40))
        fill(ctx, polygon(gem), color(0xFFF4FD))
        fill(ctx, polygon([(42, 95), (51, 73), (23, 73)]), color(0xFFD2EF))
        fill(ctx, polygon([(42, 95), (64, 95), (51, 73)]), color(0xF987DF))
        fill(ctx, polygon([(86, 95), (105, 73), (77, 73)]), color(0xFFD2EF))
        fill(ctx, polygon([(23, 73), (51, 73), (64, 24)]), color(0xF688D9))
        fill(ctx, polygon([(51, 73), (77, 73), (64, 24)]), color(0xFFF0FB))
        fill(ctx, polygon([(77, 73), (105, 73), (64, 24)]), color(0xCB318D))
        outline(ctx, polygon(gem), color(0xFFF8FD), 2.3)
    case 5:
        line(ctx, [(77, 50), (99, 28)], color(0x124454), width: 14)
        fill(ctx, disk(58, 69, 34), color(0x085061))
        outline(ctx, disk(58, 69, 31), color(0xD0FFF1), 4.5)
        line(ctx, [(39, 66), (48, 66), (55, 78), (62, 62), (68, 71), (77, 71)], color(0xC2FFED), width: 5)
    case 6:
        // Four targets with a bright active square convey scans across a fleet.
        for (x, y, shade) in [(39.0, 66.0, UInt32(0xFFF3FF)), (66.0, 66.0, 0xCFFF70),
                              (39.0, 39.0, 0xE0C3FF), (66.0, 39.0, 0xFFADF0)] {
            fill(ctx, box(CGFloat(x), CGFloat(y) - 3, 23, 23, 6), color(0x3F1776, 0.53))
            fill(ctx, box(CGFloat(x), CGFloat(y), 23, 23, 6), color(shade))
        }
    case 7:
        // Ray, lamp and pedestal form a clear tower silhouette at taskbar size.
        fill(ctx, polygon([(64, 86), (29, 80), (29, 62), (64, 73)]), color(0x77DFFF))
        fill(ctx, polygon([(64, 86), (99, 80), (99, 62), (64, 73)]), color(0x8BCDFF))
        fill(ctx, disk(64, 80, 12), color(0xFFF18A))
        fill(ctx, polygon([(56, 68), (72, 68), (79, 34), (49, 34)]), color(0xF2FAFF))
        fill(ctx, polygon([(54, 57), (74, 57), (76, 48), (52, 48)]), color(0x9ADFFD))
        fill(ctx, box(40, 29, 48, 8, 4), color(0xEDF8FF))
    default:
        let bolt = [(73.0, 104.0), (35.0, 59.0), (59.0, 59.0), (50.0, 24.0),
                    (94.0, 72.0), (67.0, 72.0)].map { (CGFloat($0.0), CGFloat($0.1)) }
        fill(ctx, disk(64, 63, 43), color(0xA2194F, 0.59))
        outline(ctx, disk(64, 63, 41), color(0xFFB8A2, 0.7), 2)
        gradient(ctx, in: polygon(bolt), [color(0xFFF9CE), color(0xFFD45B)], from: CGPoint(x: 67, y: 105), to: CGPoint(x: 65, y: 24))
        outline(ctx, polygon(bolt), color(0xFFEDAC), 1.9)
    }
}

func bitmap(_ w: Int, _ h: Int) -> CGContext {
    CGContext(data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
              space: colorSpace, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
}

func png(_ ctx: CGContext) -> Data {
    NSBitmapImageRep(cgImage: ctx.makeImage()!).representation(using: .png, properties: [:])!
}

func image(_ index: Int, size: Int) -> CGContext {
    let ctx = bitmap(size, size)
    drawIcon(ctx, index: index, frame: CGRect(x: 0, y: 0, width: size, height: size))
    return ctx
}

extension Data {
    mutating func writeLE<T: FixedWidthInteger>(_ value: T) {
        var little = value.littleEndian
        Swift.withUnsafeBytes(of: &little) { append(contentsOf: $0) }
    }
}

// Use BGRA DIB frames with Windows AND masks, matching the established resource format.
func dib(_ ctx: CGContext, side: Int) -> Data {
    var data = Data()
    for n in [40, side, side * 2] { data.writeLE(UInt32(n)) }
    data.writeLE(UInt16(1))
    data.writeLE(UInt16(32))
    for n in [0, side * side * 4, 0, 0, 0, 0] { data.writeLE(UInt32(n)) }
    let bytes = ctx.data!.assumingMemoryBound(to: UInt8.self)
    let maskStride = ((side + 31) / 32) * 4
    var mask = [UInt8](repeating: 0, count: maskStride * side)
    for y in 0..<side {
        for x in 0..<side {
            let start = ((side - 1 - y) * side + x) * 4
            let a = Int(bytes[start + 3])
            let straight = { (channel: Int) in a == 0 ? 0 : min(255, (Int(bytes[start + channel]) * 255 + a / 2) / a) }
            for channel in [2, 1, 0] { data.append(UInt8(straight(channel))) }
            data.append(UInt8(a))
            if a == 0 { mask[y * maskStride + x / 8] |= 0x80 >> (x % 8) }
        }
    }
    data.append(contentsOf: mask)
    return data
}

let iconSizes = [16, 24, 32, 48, 64, 128, 256]
func iconFile(_ frames: [Data]) -> Data {
    var icon = Data()
    icon.writeLE(UInt16(0))
    icon.writeLE(UInt16(1))
    icon.writeLE(UInt16(iconSizes.count))
    var offset = 6 + 16 * iconSizes.count
    for (side, frame) in zip(iconSizes, frames) {
        icon.append(contentsOf: [UInt8(side % 256), UInt8(side % 256), 0, 0])
        icon.writeLE(UInt16(1))
        icon.writeLE(UInt16(32))
        icon.writeLE(UInt32(frame.count))
        icon.writeLE(UInt32(offset))
        offset += frame.count
    }
    for frame in frames { icon.append(frame) }
    return icon
}

func text(_ ctx: CGContext, _ string: String, x: CGFloat, y: CGFloat, size: CGFloat, ink: UInt32, strong: Bool = false) {
    let font = CTFontCreateWithName((strong ? "PingFangSC-Semibold" : "PingFangSC-Regular") as CFString, size, nil)
    let line = CTLineCreateWithAttributedString(NSAttributedString(string: string, attributes: [
        NSAttributedString.Key(kCTFontAttributeName as String): font,
        NSAttributedString.Key(kCTForegroundColorAttributeName as String): color(ink),
    ]))
    ctx.saveGState()
    ctx.textMatrix = .identity
    ctx.textPosition = CGPoint(x: x, y: y)
    CTLineDraw(line, ctx)
    ctx.restoreGState()
}

let directory = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
if CommandLine.arguments.contains("--round2") {
    let board = bitmap(1056, 676)
    let frame = CGRect(x: 0, y: 0, width: 1056, height: 676)
    gradient(board, in: CGPath(rect: frame, transform: nil), [color(0xEDF0FF), color(0xEFF7F7)], from: CGPoint(x: 0, y: 675), to: CGPoint(x: 1056, y: 0))
    text(board, "BTC Scanning tool", x: 26, y: 623, size: 36, ink: 0x14223B, strong: true)
    text(board, "新图标 · 右侧是 32 px 和 16 px 实际大小", x: 28, y: 595, size: 16, ink: 0x4F617E)
    for index in 3..<designs.count {
        let design = designs[index]
        let renders = iconSizes.map { image(index, size: $0) }
        let frames = zip(renders, iconSizes).map { dib($0.0, side: $0.1) }
        try iconFile(frames).write(to: directory.appendingPathComponent("\(design.file).ico"))
        try png(renders[6]).write(to: directory.appendingPathComponent("\(design.file).png"))

        let card = index - 3
        let x = CGFloat(26 + 340 * (card % 3))
        let y = CGFloat(306 - 280 * (card / 3))
        let panel = box(x, y, 324, 264, 22)
        fill(board, panel, color(0xFFFFFF))
        outline(board, panel, color(0xD9E1EF), 1)
        fill(board, box(x + 19, y + 211, 35, 34, 10), color(design.accent))
        text(board, "\(index + 1)", x: x + 30, y: y + 220, size: 19, ink: 0xFFFFFF, strong: true)
        text(board, design.name, x: x + 65, y: y + 219, size: 23, ink: 0x152745, strong: true)
        text(board, design.subtitle, x: x + 22, y: y + 24, size: 15, ink: 0x536681)
        fill(board, box(x + 191, y + 113, 111, 53, 13), color(0xEBEFF8))
        fill(board, box(x + 191, y + 55, 111, 49, 13), color(0x1D293F))
        for (renderIndex, dx, dy, size) in [(5, 31, 59, 138), (2, 207, 124, 32), (0, 261, 132, 16),
                                                (2, 207, 64, 32), (0, 261, 72, 16)] {
            board.interpolationQuality = .none
            board.draw(renders[renderIndex].makeImage()!, in: CGRect(x: x + CGFloat(dx), y: y + CGFloat(dy), width: CGFloat(size), height: CGFloat(size)))
        }
        text(board, "32 px", x: x + 208, y: y + 178, size: 12, ink: 0x536681)
        text(board, "16 px", x: x + 254, y: y + 178, size: 12, ink: 0x536681)
    }
    try png(board).write(to: directory.appendingPathComponent("BTC-Scanning-tool-图标候选.png"))
    exit(EXIT_SUCCESS)
}

let sheet = bitmap(1056, 408)
let sheetFrame = CGPath(rect: CGRect(x: 0, y: 0, width: 1056, height: 408), transform: nil)
gradient(sheet, in: sheetFrame, [color(0xF5F7FF), color(0xE8EEF9)], from: CGPoint(x: 0, y: 408), to: CGPoint(x: 1056, y: 0))
text(sheet, "MineWatch 新图标", x: 26, y: 357, size: 28, ink: 0x142742, strong: true)
text(sheet, "选择你喜欢的一款，右侧小图展示任务栏尺寸。", x: 27, y: 326, size: 16, ink: 0x4D617A)

for (index, design) in designs.prefix(3).enumerated() {
    let renders = iconSizes.map { image(index, size: $0) }
    let frames = zip(renders, iconSizes).map { dib($0.0, side: $0.1) }
    try iconFile(frames).write(to: directory.appendingPathComponent("\(design.file).ico"))
    try png(renders[6]).write(to: directory.appendingPathComponent("\(design.file).png"))

    let x = CGFloat(26 + 340 * index)
    let panel = box(x, 27, 324, 269, 20)
    fill(sheet, panel, color(0xFFFFFF))
    outline(sheet, panel, color(0xD8E1EF), 1)
    fill(sheet, box(x + 19, 247, 34, 32, 10), color(design.accent))
    text(sheet, "\(index + 1)", x: x + 30, y: 255, size: 19, ink: 0xFFFFFF, strong: true)
    text(sheet, design.name, x: x + 65, y: 254, size: 23, ink: 0x192B45, strong: true)
    text(sheet, design.subtitle, x: x + 20, y: 54, size: 15, ink: 0x536681)
    fill(sheet, box(x + 201, 112, 101, 54, 13), color(0xE9EDF6))
    fill(sheet, box(x + 252, 112, 50, 54, 13), color(0x1B263D))
    for (renderIndex, dx, y, size) in [(5, 35, 93, 128), (2, 210, 123, 32), (2, 261, 123, 32)] {
        sheet.interpolationQuality = .none
        sheet.draw(renders[renderIndex].makeImage()!, in: CGRect(x: x + CGFloat(dx), y: CGFloat(y), width: CGFloat(size), height: CGFloat(size)))
    }
    text(sheet, "32 px", x: x + 231, y: 92, size: 12, ink: 0x4D617A)
}
try png(sheet).write(to: directory.appendingPathComponent("MineWatch-图标候选.png"))
