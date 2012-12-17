// Copyright 2012 Google, Inc. All rights reserved.
// Copyright 2009-2012 Andreas Krennmair. All rights reserved.

package layers

import (
	"encoding/binary"
	"github.com/gconnell/gopacket"
)

// UDP is the layer for UDP headers.
type UDP struct {
	SrcPort      uint16
	DstPort      uint16
	Length       uint16
	Checksum     uint16
	sPort, dPort []byte
}

// LayerType returns gopacket.LayerTypeUDP
func (u *UDP) LayerType() gopacket.LayerType { return LayerTypeUDP }

func decodeUDP(data []byte) (out gopacket.DecodeResult, err error) {
	udp := &UDP{
		SrcPort:  binary.BigEndian.Uint16(data[0:2]),
		sPort:    data[0:2],
		DstPort:  binary.BigEndian.Uint16(data[2:4]),
		dPort:    data[2:4],
		Length:   binary.BigEndian.Uint16(data[4:6]),
		Checksum: binary.BigEndian.Uint16(data[6:8]),
	}
	out.DecodedLayer = udp
	out.NextDecoder = gopacket.LayerTypePayload
	out.RemainingBytes = data[8:]
	out.TransportLayer = udp
	return
}

func (u *UDP) TransportFlow() gopacket.Flow {
	return gopacket.NewFlow(EndpointUDPPort, u.sPort, u.dPort)
}