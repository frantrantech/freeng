package webRTCHelpers 

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/pion/webrtc/v4"
	"io"
	"os"
	"strings"
)

/* JSON encode + base64 a SessionDescription */
func Encode(obj *webrtc.SessionDescription) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(b)
}

/* Read in contents from standard in and returns it*/
func ReadUntilNewline() (in string) {
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
func Decode(in string, obj *webrtc.SessionDescription) {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		panic(err)
	}

	if err = json.Unmarshal(b, obj); err != nil {
		panic(err)
	}
}
