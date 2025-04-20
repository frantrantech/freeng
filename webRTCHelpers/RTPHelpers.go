package webRTCHelpers

import (
	"fmt"
	"github.com/pion/rtp"
)

func LogRTPPacket(rtpPacket rtp.Packet, counter int) {
	if counter > 50 {
		return
	}
	fmt.Printf("Received RTP packet:\n")
	fmt.Printf("  SSRC: %d\n", rtpPacket.SSRC)                      // ID of where the packet is coming from
	fmt.Printf("  Marker Bit: %t\n", rtpPacket.Marker)              // ID of where the packet is coming from
	fmt.Printf("  Payload Type: %d\n", rtpPacket.PayloadType)       // ID of where the packet is coming from
	fmt.Printf("  Sequence Number: %d\n", rtpPacket.SequenceNumber) // Incremental counter from zero
	fmt.Printf("  Timestamp: %d\n", rtpPacket.Timestamp)
	fmt.Printf("  Payload size: %d bytes\n", len(rtpPacket.Payload)) // Actual RTP data
	fmt.Println("")
}
