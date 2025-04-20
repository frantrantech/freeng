package detection

import (
	// "fmt"
	// "io"
	"fmt"
	helper "freeng/webRTCHelpers"
	"github.com/pion/rtp"
	"gocv.io/x/gocv"
	"io"
	"log"
	"net"
	"os/exec"
)

/*
Take in a feed and detect movement
Need to be able to ingest:
  from ffmpeg
  from rtp on site
*/

const (
	frameX      = 960
	frameY      = 720
	frameSize   = frameX * frameY * 3
	minimumArea = 3000
)

/*
  General flow of gocv:
  Define a "Mat" object that will represent one frame/image. Its an n dimensional array with m color channels.
    Given RGB format -> 3 color channels -> go.MatTypeCV8U3
    This is a Mat that has 3 channels where each value is an uint8. (Recall uint8's max val is 255. Since 2^8 is 256. int8 would be 127)
  Convert packets from a stream / video source into a byte array which can be converted to a Mat
    buf := make([]byte, frameSize)
    gocv.NewMatFromBytes(frameY, frameX, gocv.MatTypeCV8UC3, buf)
  Process the Mat
    ...
*/

func Detect(server net.PacketConn) {
	window := gocv.NewWindow("Motion Window")
	defer window.Close() //nolint

	img := gocv.NewMat()
	defer img.Close() //nolint

	imgDelta := gocv.NewMat()
	defer imgDelta.Close() //nolint

	imgThresh := gocv.NewMat()
	defer imgThresh.Close() //nolint

	mog2 := gocv.NewBackgroundSubtractorMOG2()
	defer mog2.Close() //nolint

	// readServerPackets(server)
	// ffmpeg -re -i testFeed.mov -c:v libx264 -f rtp rtp://127.0.0.1:5004
	// ffmpeg := exec.Command("ffmpeg", "-re", "-i", "testFeed.mov", "-c:v", "libx264", "-f", "rtp", "rtp://127.0.0.1:5004")

	ffmpeg := exec.Command(
		"ffmpeg",
		"-protocol_whitelist", "file,udp,rtp",
		"-i", "input.sdp",
		"-pix_fmt", "bgr24",
		"-s", fmt.Sprintf("%dx%d", frameX, frameY),
		"-f", "rawvideo",
		"pipe:1",
	)

	ffmpegOut, _ := ffmpeg.StdoutPipe()
	// ffmpegIn,_ := ffmpeg.StdinPipe()
	// ffmpegErr,_ := ffmpeg.StderrPipe()
	if err := ffmpeg.Start(); err != nil {
		panic(err)
	}
	for {
		buf := make([]byte, frameSize)
		if _, err := io.ReadFull(ffmpegOut, buf); err != nil {
			fmt.Printf("Error with ffmpeg out: %e", err)
			continue
		}
		img, _ = gocv.NewMatFromBytes(frameX, frameY, gocv.MatTypeCV8UC3, buf)
		if img.Empty() {
      fmt.Println("Img empty")
			continue
		}
		window.IMShow(img)
		window.WaitKey(1)
	}
}

/*
	Take read in RTP packets from our UDP server, convert to a gocv.MatTypeCV8UC3

SEND ONLY VIDEO TO SERVER: ffmpeg -re -i testFeed.mov -c:v libx264 -f rtp rtp://127.0.0.1:5004
LOOK AT FEED: ffplay -protocol_whitelist file,rtp,udp -i s.sdp

Full rtp packet cannot contain all data for a full frame.
A full frame is:

	A group of RTP packets with the same timestamp
	The end of the frame is the timestamp with the marker bit set.
	We are not guaranteed all timestamps to have market bit set.

To get a buffer with that has all the rtp packets for a full frame:
Read in RTP packets from server

	Find boundary where to start? Should we start after first marker bit set, or should we start at next timestamp?
	  ffmpeg guarantees we recieve packets in oder of timestamp. lets do this for now

	Add packets with the same time stamp to an array
	Recieve the timestamp with the marker bit
	Ensure that rtpPacketTimestamp[] is sorted
	Convert rtpPacketTimestamp[] to gocv.MatTypeCV8UC3
*/

var FIRST_PACKET_NOT_SEEN uint32 = 0
var INITIAL_LAST_PACKET uint32 = 0

func readServerPackets(server net.PacketConn) {
	packet := make([]byte, 1500)
	firstPacketTimestamp := FIRST_PACKET_NOT_SEEN
	canStartTrackingPackets := false
	lastPacketTimestamp := INITIAL_LAST_PACKET
	counter := 0

	for {
		numBytesInPacket, readError := readRTPPacket(server, packet)
		if readError != nil {
			continue
		}
		var currRTPPacket rtp.Packet
		currPacketTimestamp := currRTPPacket.Timestamp
		if err := currRTPPacket.Unmarshal(packet[:numBytesInPacket]); err != nil {
			log.Printf("Failed to unmarshal RTP packet: %v", err)
		}

		/* This is the first packet we've seen. Store the timnestamp*/
		if firstPacketTimestamp == FIRST_PACKET_NOT_SEEN {
			firstPacketTimestamp = currPacketTimestamp
		}
		/* If this isn't the first packet we've seen and the currentPacket has a larger time stamp, we can begin tracking packets.*/
		if firstPacketTimestamp != FIRST_PACKET_NOT_SEEN && currPacketTimestamp > firstPacketTimestamp {
			canStartTrackingPackets = true
		}
		/* Stop if we haven't a timestamp different from the inital timestamp*/
		if !canStartTrackingPackets {
			continue
		}

		/* Start a new packet block */
		if lastPacketTimestamp < currPacketTimestamp {

		}

		helper.LogRTPPacket(currRTPPacket, counter)
		lastPacketTimestamp = currRTPPacket.Timestamp
		counter++
	}
}

/* Populate our buffer with the rtp packet that our UDP server recieved */
func readRTPPacket(conn net.PacketConn, packet []byte) (int, error) {
	numBytesInPacket, _, err := conn.ReadFrom(packet)
	if err != nil {
		log.Printf("Error reading from connection: %v", err)
	}
	return numBytesInPacket, err
}
