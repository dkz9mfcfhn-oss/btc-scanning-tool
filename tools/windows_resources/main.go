// Command windows_resources builds the COFF object linked into Windows releases.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/akavel/rsrc/binutil"
	"github.com/akavel/rsrc/coff"
	"github.com/akavel/rsrc/ico"
)

const rtVersion = 16

func main() {
	iconPath := flag.String("ico", "", "input Windows ICO")
	output := flag.String("o", "", "output Windows amd64 .syso file")
	name := flag.String("name", "BTC Scanning tool", "file description and product name")
	version := flag.String("version", "", "four-part file and product version")
	flag.Parse()
	if *iconPath == "" || *output == "" || *version == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := writeResources(*iconPath, *output, *name, *version); err != nil {
		log.Fatal(err)
	}
}

func writeResources(iconPath, output, name, version string) error {
	versionInfo, err := versionResource(name, version)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(iconPath)
	if err != nil {
		return err
	}
	icons, err := ico.DecodeHeaders(bytes.NewReader(data))
	if err != nil || len(icons) == 0 || len(icons) > 256 {
		return fmt.Errorf("invalid icon directory in %s", iconPath)
	}
	object := coff.NewRSRC()
	if err := object.Arch("amd64"); err != nil {
		return err
	}
	group := bytes.NewBuffer(nil)
	appendLE(group, ico.ICONDIR{Type: 1, Count: uint16(len(icons))})
	for index, icon := range icons {
		start := uint64(icon.ImageOffset)
		end := start + uint64(icon.BytesInRes)
		if end <= start || end > uint64(len(data)) {
			return fmt.Errorf("icon image %d exceeds the input file", index+1)
		}
		imageID := uint16(index + 2)
		object.AddResource(coff.RT_ICON, imageID, bytes.NewReader(data[start:end]))
		appendLE(group, icon.IconDirEntryCommon)
		appendLE(group, imageID)
	}
	object.AddResource(coff.RT_GROUP_ICON, 1, bytes.NewReader(group.Bytes()))
	object.AddResource(rtVersion, 1, bytes.NewReader(versionInfo))
	object.Freeze()

	var dataOut bytes.Buffer
	writer := binutil.Writer{W: &dataOut}
	if err := binutil.Walk(object, func(value reflect.Value, _ string) error {
		if reader, ok := value.Interface().(binutil.SizedReader); ok {
			writer.WriteFromSized(reader)
			return binutil.WALK_SKIP
		}
		if binutil.Plain(value.Kind()) {
			writer.WriteLE(value.Interface())
		}
		return writer.Err
	}); err != nil {
		return err
	}
	if writer.Err != nil {
		return writer.Err
	}
	return os.WriteFile(output, dataOut.Bytes(), 0644)
}

func versionResource(name, version string) ([]byte, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 4 || name == "" || strings.ContainsRune(name, 0) || len(name) > 256 {
		return nil, fmt.Errorf("expected a product name and a four-part version")
	}
	var numbers [4]uint32
	for index, part := range parts {
		number, err := strconv.ParseUint(part, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid version %q: %w", version, err)
		}
		numbers[index] = uint32(number)
	}
	versionHigh := numbers[0]<<16 | numbers[1]
	versionLow := numbers[2]<<16 | numbers[3]
	fixedInfo := bytes.NewBuffer(nil)
	appendLE(fixedInfo, [13]uint32{
		0xfeef04bd, 0x00010000, // Signature and fixed structure version.
		versionHigh, versionLow, versionHigh, versionLow,
		0x3f, 0, 0x00040004, 1, 0, 0, 0, // Flags, NT Win32, application, subtype, timestamp.
	})
	strings := versionBlock("StringFileInfo", nil, true, versionBlock("040904b0", nil, true,
		versionBlock("FileDescription", wideBytes(name), true),
		versionBlock("FileVersion", wideBytes(version), true),
		versionBlock("InternalName", wideBytes(name), true),
		versionBlock("OriginalFilename", wideBytes(name+".exe"), true),
		versionBlock("ProductName", wideBytes(name), true),
		versionBlock("ProductVersion", wideBytes(version), true),
	))
	translation := bytes.NewBuffer(nil)
	appendLE(translation, [2]uint16{0x0409, 1200}) // US English, Unicode.
	vars := versionBlock("VarFileInfo", nil, true, versionBlock("Translation", translation.Bytes(), false))
	return versionBlock("VS_VERSION_INFO", fixedInfo.Bytes(), false, strings, vars), nil
}

// Version blocks are DWORD aligned, with text lengths counted in UTF-16 code
// units. A block's length includes its children and excludes padding after it.
// https://learn.microsoft.com/en-us/windows/win32/menurc/vs-versioninfo
func versionBlock(key string, value []byte, text bool, children ...[]byte) []byte {
	var buffer bytes.Buffer
	valueLength, kind := len(value), uint16(0)
	if text {
		valueLength /= 2
		kind = 1
	}
	appendLE(&buffer, [3]uint16{0, uint16(valueLength), kind})
	buffer.Write(wideBytes(key))
	buffer.Write(make([]byte, -buffer.Len()&3))
	buffer.Write(value)
	for _, child := range children {
		buffer.Write(make([]byte, -buffer.Len()&3))
		buffer.Write(child)
	}
	data := buffer.Bytes()
	binary.LittleEndian.PutUint16(data, uint16(len(data)))
	return data
}

func wideBytes(value string) []byte {
	var buffer bytes.Buffer
	appendLE(&buffer, append(utf16.Encode([]rune(value)), 0))
	return buffer.Bytes()
}

func appendLE(buffer *bytes.Buffer, data any) {
	// Every caller supplies fixed-width primitive binary data.
	if err := binary.Write(buffer, binary.LittleEndian, data); err != nil {
		panic(err)
	}
}
