package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

func main() {

	/* Initalize UDP server to read RTP packets*/
	address := "0.0.0.0:5004"
	conn, err := net.ListenPacket("udp", address)
	if err != nil {
		log.Fatalf("Failed to listen on UDP: %v", err)
	}
	defer conn.Close()

	fmt.Printf("RTP server listening on %s\n", address)

	/* Pion WebRTC Code */
	mediaEngine := &webrtc.MediaEngine{}
	codecErr := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
			RTCPFeedback: []webrtc.RTCPFeedback{
				{Type: "goog-remb"},
				{Type: "ccm", Parameter: "fir"},
				{Type: "nack"},
				{Type: "nack", Parameter: "pli"},
			},
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo)

	if codecErr != nil {
		panic(err)
	}

	interceptorRegistry := &interceptor.Registry{}
	if intRegErr := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		panic(intRegErr)
	}

	intervalPLIFactory, newRecInterErr := intervalpli.NewReceiverInterceptor()
	if newRecInterErr != nil {
		panic(newRecInterErr)
	}

	interceptorRegistry.Add(intervalPLIFactory)
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
	)

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	peerConnection, peerConnErr := api.NewPeerConnection(config)
	if peerConnErr != nil {
		panic(peerConnErr)
	}

	/* Finally, close connection with peer*/
	defer func() {
		if connErr := peerConnection.Close(); connErr != nil {
			fmt.Printf("Cannot close peer connection: %v", connErr)
		}
	}()

	outputTrack, trackLocalInitErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if trackLocalInitErr != nil {
		panic(trackLocalInitErr)
	}

	rtpSender, addTrackErr := peerConnection.AddTrack(outputTrack)
	if addTrackErr != nil {
		panic(addTrackErr)
	}

	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			/* Place contents of rtpSender into rtcp buffer */
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	/* Wait for us to get offer
	   We reading in an encoded SDP offer and decode it
	*/
	offer := webrtc.SessionDescription{}
	decode(readUntilNewline(), &offer)

	/* Have the other client's SDP offer, pass it to our peerConnection*/
	remoteDescErr := peerConnection.SetRemoteDescription(offer)
	if remoteDescErr != nil {
		panic(remoteDescErr)
	}

	peerConnection.OnTrack(
		func(track *webrtc.TrackRemote, reciever *webrtc.RTPReceiver) {
			fmt.Printf("We have recieved a track, it is type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
			// for {
			// 	rtp, _, readErr := track.ReadRTP()
			// 	if readErr != nil {
			// 		panic(readErr)
			// 	}
			// 	if writeErr := outputTrack.WriteRTP(rtp); writeErr != nil {
			// 		panic(writeErr)
			// 	}
			// }
		},
	)

	peerConnection.OnConnectionStateChange(
		func(connState webrtc.PeerConnectionState) {
			fmt.Printf("Peer connection state has changed: %s\n", connState.String())

			if connState == webrtc.PeerConnectionStateFailed {
				fmt.Println("Peer Connection has gone to failed exiting")
				os.Exit(0)
			}

			if connState == webrtc.PeerConnectionStateClosed {
				fmt.Println("Peer Connection has gone to closed exiting")
				os.Exit(0)
			}
		},
	)

	answer, answerErr := peerConnection.CreateAnswer(nil)
	if answerErr != nil {
		panic(answerErr)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	if localDescErr := peerConnection.SetLocalDescription(answer); localDescErr != nil {
		panic(localDescErr)
	}

	<-gatherComplete
	print(encode(peerConnection.LocalDescription()))

	/* Constantly read packets from our udp server*/

	go processCameraFeed(conn, outputTrack)

	select {}
}

func processCameraFeed(conn net.PacketConn, outputTrack *webrtc.TrackLocalStaticRTP) {
	packet := make([]byte, 1500)
	for {
		/* Populate Buffer with rtp packets retrieved from rtsp server sent from camera */
		numBytesInPacket, _, err := conn.ReadFrom(packet)
		if err != nil {
			log.Printf("Error reading from connection: %v", err)
			continue
		}

		/* Process Packet, unmarshal only the bytes that the packet contains */
		var rtpPacket rtp.Packet
		if err := rtpPacket.Unmarshal(packet[:numBytesInPacket]); err != nil {
			log.Printf("Failed to unmarshal RTP packet: %v", err)
			continue
		}

		/* Place the rtp packet into writer */
		if writeErr := outputTrack.WriteRTP(&rtpPacket); writeErr != nil {
			panic(writeErr)
		}
		fmt.Printf("Received RTP packet:\n")
		fmt.Printf("  SSRC: %d\n", rtpPacket.SSRC)
		// fmt.Printf("  Sequence Number: %d\n", rtpPacket.SequenceNumber)
		// fmt.Printf("  Timestamp: %d\n", rtpPacket.Timestamp)
		// fmt.Printf("  Payload size: %d bytes\n", len(rtpPacket.Payload))
	}
}

// JSON encode + base64 a SessionDescription
func encode(obj *webrtc.SessionDescription) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(b)
}

/* Read in contents from standard in and returns it*/
func readUntilNewline() (in string) {
	var err error

	r := bufio.NewReader(os.Stdin)
	for {
		in, err = r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			panic(err)
		}

		if in = strings.TrimSpace(in); len(in) > 0 {
			break
		}
	}
	return
}

/*
Takes in an encoded session description as a string and
unmarshals it into obj
*/
func decode(in string, obj *webrtc.SessionDescription) {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		panic(err)
	}

	if err = json.Unmarshal(b, obj); err != nil {
		panic(err)
	}
}
