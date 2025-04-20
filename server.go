package main

import (
	"fmt"
	detection "freeng/detection"
	helper "freeng/webRTCHelpers"
	"io"
	"log"
	"net"
	"os"
	"os/exec"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

/*
	Initialize a UDP server that reads RTP packets from cameras on prem

1) Forward all RTP packets to a peer
2) Process video from RTP to see if there is movement
*/
func main() {

	// var server net.PacketConn
	// /* Initalize UDP server to read RTP packets*/
	// // address := "127.0.0.1:5004"
	// server, err := net.ListenPacket("udp", address)
	// if err != nil {
	// 	log.Fatalf("Failed to listen on UDP: %v", err)
	// }
	// defer server.Close()
	// fmt.Printf("RTP server listening on %s\n", address)

	/* Start video detection */
	// detection.Detect()
	go detection.Detect()

	ffmpegRTPPassthrough := exec.Command(
		"ffmpeg",
		"-protocol_whitelist", "file,udp,rtp",
		"-i", "input.sdp",
		"-c", "copy", // No decoding or re-encoding
		"-f", "rtp", // Output format is RTP
		"pipe:1", // Write raw RTP packets to stdout
	)
	ffmpegOut, _ := ffmpegRTPPassthrough.StdoutPipe()
	if err := ffmpegRTPPassthrough.Start(); err != nil {
		// panic(err)
	}

	fmt.Println("Starting rtp forward")

	/* Pion WebRTC Code */
	mediaEngine := &webrtc.MediaEngine{}
	codecErr := mediaEngine.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			// MimeType:    webrtc.MimeTypeH264,
			MimeType:    webrtc.MimeTypeVP8,
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
		panic(codecErr)
	}

	/* Set up interceptors */
	interceptorRegistry := &interceptor.Registry{}
	if intRegErr := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); intRegErr != nil {
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

	/* Connection config for connecting with other clients */
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	/* Create connection object, this object defines our logic whenever we recieve a track / how we send a track */
	peerConnection, peerConnErr := api.NewPeerConnection(config)
	if peerConnErr != nil {
		panic(peerConnErr)
	}

	/* At program end, close connection with peers*/
	defer func() {
		if connErr := peerConnection.Close(); connErr != nil {
			fmt.Printf("Cannot close peer connection: %v", connErr)
		}
	}()

	/* Create a buffer that stores the track that we are sending */
	outputTrack, trackLocalInitErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	if trackLocalInitErr != nil {
		panic(trackLocalInitErr)
	}

	/* Generate an rtpSender that by adding our outputTrack to our connection object, used to send rtp packets to peers */
	rtpSender, addTrackErr := peerConnection.AddTrack(outputTrack)
	if addTrackErr != nil {
		panic(addTrackErr)
	}

	/* Constantly read rtcp packets from rtpSender
	   rtcp: control. Monitors rtp data for rtpSender */
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	/* Wait for us to get offer
	   We reading in an encoded SDP offer and decode it */
	offer := webrtc.SessionDescription{}
	helper.Decode(helper.ReadUntilNewline(), &offer)

	/* Have the other client's SDP offer, pass it to our peerConnection*/
	remoteDescErr := peerConnection.SetRemoteDescription(offer)
	if remoteDescErr != nil {
		panic(remoteDescErr)
	}

	/* Logic for us recieving a track from a peer */
	peerConnection.OnTrack(
		func(track *webrtc.TrackRemote, reciever *webrtc.RTPReceiver) {
			fmt.Printf("we have recieved a track, it is type %d: %s \n", track.PayloadType(), track.Codec().MimeType)
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

	/* Logic for handling connection state with other a peer */
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

	/* Get an answer that represents if we can connect to a peer or not */
	answer, answerErr := peerConnection.CreateAnswer(nil)
	if answerErr != nil {
		panic(answerErr)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	if localDescErr := peerConnection.SetLocalDescription(answer); localDescErr != nil {
		panic(localDescErr)
	}

	/* Wait until gatherComplete channel has information on if we have connected a peer or not */
  <-gatherComplete

  fmt.Println()
  fmt.Println(helper.Encode(peerConnection.LocalDescription()))
	fmt.Println()
	fmt.Println()

	// go processCameraFeed(conn, outputTrack,peerConnection)
	go processCameraPackets(ffmpegOut, outputTrack)

	select {}
}

func processCameraPackets(ffmpegOut io.ReadCloser, outputTrack *webrtc.TrackLocalStaticRTP) {
	packet := make([]byte, 1500)
  fmt.Println("Starting rtp processing")
	for {
		// print(helper.Encode(peerConnection.LocalDescription()))
		numBytesInPacket, readError := readFFmpegRTPPacket(ffmpegOut, packet)
		if readError != nil {
			continue
		}
		var rtpPacket rtp.Packet
		if err := rtpPacket.Unmarshal(packet[:numBytesInPacket]); err != nil {
			// log.Printf("Failed to unmarshal RTP packet: %v", err)
		}
		sendRTPPacket(outputTrack, rtpPacket)
		helper.LogRTPPacket(rtpPacket, 0)
	}
}

func readFFmpegRTPPacket(ffmpegOut io.ReadCloser, packet []byte) (int, error) {
	numBytesInPacket, err := io.ReadFull(ffmpegOut, packet)
	if err != nil {
		// fmt.Println(err)
	}

	return numBytesInPacket, err
}

/* PROGRESS: Can start udp server and recieve packets while we have a webrtc connection
   Peer is not getting video it seems however, could be a codec issue or a sending packet issue
*/

/*
Recieve rtp packets from our server
Send rtp packets to peers by placing track into outputTrack
Detect if there is movement
*/

// func processCameraFeed(conn net.PacketConn, outputTrack *webrtc.TrackLocalStaticRTP, peerConnection *webrtc.PeerConnection) {
func processServerCameraFeed(server net.PacketConn, outputTrack *webrtc.TrackLocalStaticRTP) {
	packet := make([]byte, 1500)
	for {
		numBytesInPacket, readError := readServerRTPPacket(server, packet)
		if readError != nil {
			continue
		}
		var rtpPacket rtp.Packet
		if err := rtpPacket.Unmarshal(packet[:numBytesInPacket]); err != nil {
			log.Printf("Failed to unmarshal RTP packet: %v", err)
		}
		sendRTPPacket(outputTrack, rtpPacket)
		helper.LogRTPPacket(rtpPacket, 0)
	}
}

/* Populate our buffer with the rtp packet that our UDP server recieved */
func readServerRTPPacket(server net.PacketConn, packet []byte) (int, error) {
	numBytesInPacket, _, err := server.ReadFrom(packet)
	if err != nil {
		log.Printf("Error reading from connection: %v", err)
	}
	return numBytesInPacket, err
}

/* Process Packet, unmarshal only the bytes that the packet contains */
func processRTPPacket(numBytesInPacket int, packet []byte) rtp.Packet {
	var rtpPacket rtp.Packet
	if err := rtpPacket.Unmarshal(packet[:numBytesInPacket]); err != nil {
		log.Printf("Failed to unmarshal RTP packet: %v", err)
	}
	return rtpPacket
}

/* Place the rtp packet into writer so it can be sent to peers*/
func sendRTPPacket(outputTrack *webrtc.TrackLocalStaticRTP, rtpPacket rtp.Packet) {
	if writeErr := outputTrack.WriteRTP(&rtpPacket); writeErr != nil {
		panic(writeErr)
	}
}
