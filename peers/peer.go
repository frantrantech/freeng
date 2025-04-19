package peers

import (
	"fmt"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"os"
    helper "freeng/webRTCHelpers"
)

func Peer() {

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
	print(helper.Encode(peerConnection.LocalDescription()))

	/* Constantly read packets from our udp server */
	select {}
}
