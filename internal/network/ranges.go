// Package network expands explicit private IPv4 scan targets. It never performs
// discovery outside the ranges entered by the user.
package network

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const MaxTargets = 4096

func privateIPv4(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil || !a.Is4() || !a.IsPrivate() {
		return netip.Addr{}, fmt.Errorf("仅支持局域网私有 IPv4 地址 (10.x、172.16–31.x、192.168.x)：%s", s)
	}
	return a, nil
}

func number(a netip.Addr) uint32 { b := a.As4(); return binary.BigEndian.Uint32(b[:]) }
func address(n uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return netip.AddrFrom4(b)
}

// ExpandRanges accepts single IPs, CIDR blocks, full IP ranges, and abbreviated
// last-octet ranges. Results are numerically sorted and de-duplicated.
func ExpandRanges(input string) ([]string, error) {
	if len(input) > 64*1024 {
		return nil, fmt.Errorf("IP 范围输入过长")
	}
	tokens := strings.FieldsFunc(input, func(r rune) bool { return unicode.IsSpace(r) || r == ',' || r == ';' || r == '，' || r == '；' })
	if len(tokens) == 0 {
		return nil, fmt.Errorf("请填写需要扫描的 IP 地址或网段")
	}
	if len(tokens) > MaxTargets {
		return nil, fmt.Errorf("扫描范围条目超过 %d 个", MaxTargets)
	}
	seen := make(map[uint32]struct{})
	for _, token := range tokens {
		var first, last uint32
		switch {
		case strings.Contains(token, "/"):
			p, err := netip.ParsePrefix(token)
			if err != nil || !p.Addr().Is4() {
				return nil, fmt.Errorf("无效 IPv4 CIDR：%s", token)
			}
			p = p.Masked()
			first = number(p.Addr())
			size := uint64(1) << uint(32-p.Bits())
			end := uint64(first) + size - 1
			if p.Bits() < 31 {
				first++
				end--
			}
			if end < uint64(first) || end-uint64(first)+1 > MaxTargets {
				return nil, fmt.Errorf("单个网段最多允许 %d 个地址：%s", MaxTargets, token)
			}
			last = uint32(end)
		case strings.Contains(token, "-"):
			parts := strings.Split(token, "-")
			if len(parts) != 2 {
				return nil, fmt.Errorf("无效 IP 范围：%s", token)
			}
			a, err := privateIPv4(parts[0])
			if err != nil {
				return nil, err
			}
			lastText := parts[1]
			if !strings.Contains(lastText, ".") {
				octet, err := strconv.Atoi(lastText)
				if err != nil || octet < 0 || octet > 255 {
					return nil, fmt.Errorf("无效 IP 末段：%s", token)
				}
				b := a.As4()
				b[3] = byte(octet)
				lastText = netip.AddrFrom4(b).String()
			}
			b, err := privateIPv4(lastText)
			if err != nil {
				return nil, err
			}
			first, last = number(a), number(b)
		default:
			a, err := privateIPv4(token)
			if err != nil {
				return nil, err
			}
			first, last = number(a), number(a)
		}
		if first > last {
			return nil, fmt.Errorf("范围起始 IP 大于结束 IP：%s", token)
		}
		if uint64(last)-uint64(first)+1 > MaxTargets {
			return nil, fmt.Errorf("单个范围最多允许 %d 个地址：%s", MaxTargets, token)
		}
		// Validate the whole bounded range before returning any targets.
		for n := uint64(first); n <= uint64(last); n++ {
			a := address(uint32(n))
			if !a.IsPrivate() {
				return nil, fmt.Errorf("扫描范围包含非私有 IPv4 地址：%s", a)
			}
			seen[uint32(n)] = struct{}{}
			if len(seen) > MaxTargets {
				return nil, fmt.Errorf("扫描目标总数不能超过 %d 台", MaxTargets)
			}
		}
	}
	values := make([]uint32, 0, len(seen))
	for n := range seen {
		values = append(values, n)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := make([]string, 0, len(values))
	for _, n := range values {
		out = append(out, address(n).String())
	}
	return out, nil
}

// LocalRanges suggests /24 subnets for active private IPv4 interfaces without
// sending network traffic. Suggestions are never automatically scanned.
func LocalRanges() []string {
	seen := map[string]bool{}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, v := range addrs {
			p, err := netip.ParsePrefix(v.String())
			if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() {
				continue
			}
			bits := 24
			if p.Bits() > bits {
				bits = p.Bits()
			}
			seen[netip.PrefixFrom(p.Addr(), bits).Masked().String()] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
