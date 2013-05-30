// Copyright 2012 Google, Inc. All rights reserved.
// Copyright 2009-2011 Andreas Krennmair. All rights reserved.
//
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE file in the root of the source
// tree.

package layers

import (
	"code.google.com/p/gopacket"
	"encoding/binary"
	"fmt"
	"net"
)

// IPv6 is the layer for the IPv6 header.
type IPv6 struct {
	// http://www.networksorcery.com/enp/protocol/ipv6.htm
	baseLayer
	Version      uint8
	TrafficClass uint8
	FlowLabel    uint32
	Length       uint16
	NextHeader   IPProtocol
	HopLimit     uint8
	SrcIP        net.IP
	DstIP        net.IP
	HopByHop     *IPv6HopByHop
	// hbh will be pointed to by HopByHop if that layer exists.
	hbh IPv6HopByHop
}

// LayerType returns LayerTypeIPv6
func (i *IPv6) LayerType() gopacket.LayerType { return LayerTypeIPv6 }
func (i *IPv6) NetworkFlow() gopacket.Flow {
	return gopacket.NewFlow(EndpointIPv6, i.SrcIP, i.DstIP)
}

const (
	IPv6HopByHopOptionJumbogram = 0xC2 // RFC 2675
)

func (ip6 *IPv6) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	ip6.Version = uint8(data[0]) >> 4
	ip6.TrafficClass = uint8((binary.BigEndian.Uint16(data[0:2]) >> 4) & 0x00FF)
	ip6.FlowLabel = binary.BigEndian.Uint32(data[0:4]) & 0x000FFFFF
	ip6.Length = binary.BigEndian.Uint16(data[4:6])
	ip6.NextHeader = IPProtocol(data[6])
	ip6.HopLimit = data[7]
	ip6.SrcIP = data[8:24]
	ip6.DstIP = data[24:40]
	ip6.HopByHop = nil
	// We initially set the payload to all bytes after 40.  ip6.Length or the
	// HopByHop jumbogram option can both change this eventually, though.
	ip6.baseLayer = baseLayer{data[:40], data[40:]}

	// We treat a HopByHop IPv6 option as part of the IPv6 packet, since its
	// options are crucial for understanding what's actually happening per packet.
	if ip6.NextHeader == IPProtocolIPv6HopByHop {
		ip6.hbh.DecodeFromBytes(ip6.payload, df)
		hbhLen := len(ip6.hbh.contents)
		// Reset IPv6 contents to include the HopByHop header.
		ip6.baseLayer = baseLayer{data[:40+hbhLen], data[40+hbhLen:]}
		ip6.HopByHop = &ip6.hbh
		if ip6.Length == 0 {
			for _, o := range ip6.hbh.Options {
				if o.OptionType == IPv6HopByHopOptionJumbogram {
					if len(o.OptionData) != 4 {
						return fmt.Errorf("Invalid jumbo packet option length")
					}
					payloadLength := binary.BigEndian.Uint32(o.OptionData)
					pEnd := int(payloadLength)
					if pEnd > len(ip6.payload) {
						df.SetTruncated()
					} else {
						ip6.payload = ip6.payload[:pEnd]
						ip6.hbh.payload = ip6.payload
					}
					return nil
				}
			}
			return fmt.Errorf("IPv6 length 0, but HopByHop header does not have jumbogram option")
		}
	}
	if ip6.Length == 0 {
		return fmt.Errorf("IPv6 length 0, but next header is %v, not HopByHop", ip6.NextHeader)
	} else {
		pEnd := int(ip6.Length)
		if pEnd > len(ip6.payload) {
			df.SetTruncated()
			pEnd = len(ip6.payload)
		}
		ip6.payload = ip6.payload[:pEnd]
	}
	return nil
}

func decodeIPv6(data []byte, p gopacket.PacketBuilder) error {
	ip6 := &IPv6{}
	err := ip6.DecodeFromBytes(data, p)
	p.AddLayer(ip6)
	p.SetNetworkLayer(ip6)
	if ip6.HopByHop != nil {
		// TODO(gconnell):  Since HopByHop is now an integral part of the IPv6
		// layer, should it actually be added as its own layer?  I'm leaning towards
		// no.
		p.AddLayer(ip6.HopByHop)
	}
	if err != nil {
		return err
	}
	if ip6.HopByHop != nil {
		return p.NextDecoder(ip6.HopByHop.NextHeader)
	}
	return p.NextDecoder(ip6.NextHeader)
}

func (i *IPv6HopByHop) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	i.ipv6ExtensionBase = decodeIPv6ExtensionBase(data)
	i.Options = i.opts[:0]
	var opt *IPv6HopByHopOption
	for d := i.contents[2:]; len(d) > 0; d = d[opt.ActualLength:] {
		i.Options = append(i.Options, IPv6HopByHopOption(decodeIPv6HeaderTLVOption(d)))
		opt = &i.Options[len(i.Options)-1]
	}
	return nil
}

func decodeIPv6HopByHop(data []byte, p gopacket.PacketBuilder) error {
	i := &IPv6HopByHop{}
	err := i.DecodeFromBytes(data, p)
	p.AddLayer(i)
	if err != nil {
		return err
	}
	return p.NextDecoder(i.NextHeader)
}

type ipv6HeaderTLVOption struct {
	OptionType, OptionLength uint8
	ActualLength             int
	OptionData               []byte
}

func decodeIPv6HeaderTLVOption(data []byte) (h ipv6HeaderTLVOption) {
	if data[0] == 0 {
		h.ActualLength = 1
		return
	}
	h.OptionType = data[0]
	h.OptionLength = data[1]
	h.ActualLength = int(h.OptionLength) + 2
	h.OptionData = data[2:h.ActualLength]
	return
}

// IPv6HopByHopOption is a TLV option present in an IPv6 hop-by-hop extension.
type IPv6HopByHopOption ipv6HeaderTLVOption

type ipv6ExtensionBase struct {
	baseLayer
	NextHeader   IPProtocol
	HeaderLength uint8
	ActualLength int
}

func decodeIPv6ExtensionBase(data []byte) (i ipv6ExtensionBase) {
	i.NextHeader = IPProtocol(data[0])
	i.HeaderLength = data[1]
	i.ActualLength = int(i.HeaderLength)*8 + 8
	i.contents = data[:i.ActualLength]
	i.payload = data[i.ActualLength:]
	return
}

// IPDecodingLayer is a DecodingLayer (see StackParser for more details)
// which decodes IP, whether v4 or v6.  It also ignores v6 extensions (besides
// HopByHop, which is stored within the IPv6 portion of the packet).
// Before reading either v4 or v6 fields, check the Version field (will be 4 or
// 6) to determine which to use.
type IPDecodingLayer struct {
	IPv4       IPv4
	IPv6       IPv6
	Version    int
	NextHeader IPProtocol
	baseLayer
}

func (i *IPDecodingLayer) DecodeFromBytes(data []byte, df gopacket.DecodeFeedback) error {
	i.Version = int(data[0] >> 4)
	i.NextHeader = 0
	contentSize := 0
	switch i.Version {
	case 4:
		err := i.IPv4.DecodeFromBytes(data, df)
		i.NextHeader = i.IPv4.Protocol
		i.baseLayer = i.IPv4.baseLayer
		return err
	case 6:
		// First, grab our IPv6 packet.
		err := i.IPv6.DecodeFromBytes(data, df)
		i.NextHeader = i.IPv6.NextHeader
		if err != nil {
			return err
		}
		// Next, skip over extensions.
		for LayerClassIPv6Extension.Contains(i.NextHeader.LayerType()) {
			if contentSize >= len(data) {
				return gopacket.ParserNoMoreBytes
			}
			extension := decodeIPv6ExtensionBase(data[contentSize:])
			contentSize += extension.ActualLength
			i.NextHeader = extension.NextHeader
		}
		i.baseLayer = baseLayer{data[:contentSize], data[contentSize:]}
		return nil
	default:
		return fmt.Errorf("Invalid first byte of IPv4/6 layer: 0x%02x", data[0])
	}
}
func (i *IPDecodingLayer) CanDecode() gopacket.LayerClass {
	return LayerClassIPNetwork
}
func (i *IPDecodingLayer) NextLayerType() gopacket.LayerType {
	return i.NextHeader.LayerType()
}

// IPv6HopByHop is the IPv6 hop-by-hop extension.
type IPv6HopByHop struct {
	ipv6ExtensionBase
	Options []IPv6HopByHopOption
	opts    [2]IPv6HopByHopOption
}

// LayerType returns LayerTypeIPv6HopByHop.
func (i *IPv6HopByHop) LayerType() gopacket.LayerType { return LayerTypeIPv6HopByHop }

// IPv6Routing is the IPv6 routing extension.
type IPv6Routing struct {
	ipv6ExtensionBase
	RoutingType  uint8
	SegmentsLeft uint8
	// This segment is supposed to be zero according to RFC2460, the second set of
	// 4 bytes in the extension.
	Reserved []byte
	// SourceRoutingIPs is the set of IPv6 addresses requested for source routing,
	// set only if RoutingType == 0.
	SourceRoutingIPs []net.IP
}

// LayerType returns LayerTypeIPv6Routing.
func (i *IPv6Routing) LayerType() gopacket.LayerType { return LayerTypeIPv6Routing }

func decodeIPv6Routing(data []byte, p gopacket.PacketBuilder) error {
	i := &IPv6Routing{
		ipv6ExtensionBase: decodeIPv6ExtensionBase(data),
		RoutingType:       data[2],
		SegmentsLeft:      data[3],
		Reserved:          data[4:8],
	}
	switch i.RoutingType {
	case 0: // Source routing
		if (len(data)-8)%16 != 0 {
			return fmt.Errorf("Invalid IPv6 source routing, length of type 0 packet %d", len(data))
		}
		for d := i.contents[8:]; len(d) >= 16; d = d[16:] {
			i.SourceRoutingIPs = append(i.SourceRoutingIPs, net.IP(d[:16]))
		}
	}
	p.AddLayer(i)
	return p.NextDecoder(i.NextHeader)
}

// IPv6Fragment is the IPv6 fragment header, used for packet
// fragmentation/defragmentation.
type IPv6Fragment struct {
	baseLayer
	NextHeader IPProtocol
	// Reserved1 is bits [8-16), from least to most significant, 0-indexed
	Reserved1      uint8
	FragmentOffset uint16
	// Reserved2 is bits [29-31), from least to most significant, 0-indexed
	Reserved2      uint8
	MoreFragments  bool
	Identification uint32
}

// LayerType returns LayerTypeIPv6Fragment.
func (i *IPv6Fragment) LayerType() gopacket.LayerType { return LayerTypeIPv6Fragment }

func decodeIPv6Fragment(data []byte, p gopacket.PacketBuilder) error {
	i := &IPv6Fragment{
		baseLayer:      baseLayer{data[:8], data[8:]},
		NextHeader:     IPProtocol(data[0]),
		Reserved1:      data[1],
		FragmentOffset: binary.BigEndian.Uint16(data[2:4]) >> 3,
		Reserved2:      data[3] & 0x6 >> 1,
		MoreFragments:  data[3]&0x1 != 0,
		Identification: binary.BigEndian.Uint32(data[4:8]),
	}
	p.AddLayer(i)
	return p.NextDecoder(i.NextHeader)
}

// IPv6DestinationOption is a TLV option present in an IPv6 destination options extension.
type IPv6DestinationOption ipv6HeaderTLVOption

// IPv6Destination is the IPv6 destination options header.
type IPv6Destination struct {
	ipv6ExtensionBase
	Options []IPv6DestinationOption
}

// LayerType returns LayerTypeIPv6Destination.
func (i *IPv6Destination) LayerType() gopacket.LayerType { return LayerTypeIPv6Destination }

func decodeIPv6Destination(data []byte, p gopacket.PacketBuilder) error {
	i := &IPv6Destination{
		ipv6ExtensionBase: decodeIPv6ExtensionBase(data),
		// We guess we'll 1-2 options, one regular option at least, then maybe one
		// padding option.
		Options: make([]IPv6DestinationOption, 0, 2),
	}
	var opt *IPv6DestinationOption
	for d := i.contents[2:]; len(d) > 0; d = d[opt.ActualLength:] {
		i.Options = append(i.Options, IPv6DestinationOption(decodeIPv6HeaderTLVOption(d)))
		opt = &i.Options[len(i.Options)-1]
	}
	p.AddLayer(i)
	return p.NextDecoder(i.NextHeader)
}