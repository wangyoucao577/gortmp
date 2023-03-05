package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	rtmp "github.com/wangyoucao577/gortmp"
	flv "github.com/zhangpeihao/goflv"
	"github.com/zhangpeihao/log"
)

const (
	programName = "RtmpPlayer"
	version     = "0.0.1"
)

var (
	url        *string = flag.String("URL", "rtmp://192.168.20.111/vid3", "The rtmp url to connect.")
	streamName *string = flag.String("Stream", "camstream", "Stream name to play.")
	dumpFlv    *string = flag.String("DumpFLV", "", "Dump FLV into file.")
)

type TestOutboundConnHandler struct {
}

var obConn rtmp.OutboundConn
var createStreamChan chan rtmp.OutboundStream
var videoDataSize int64
var audioDataSize int64
var flvFile *flv.File
var status uint

func (handler *TestOutboundConnHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	status, err = conn.Status()
	fmt.Printf("@@@@@@@@@@@@@status: %d, err: %v\n", status, err)
}

func (handler *TestOutboundConnHandler) OnClosed(conn rtmp.Conn) {
	fmt.Printf("@@@@@@@@@@@@@Closed\n")
}

func (handler *TestOutboundConnHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
	switch message.Type {
	case rtmp.VIDEO_TYPE:
		if flvFile != nil {
			flvFile.WriteVideoTag(message.Buf.Bytes(), message.AbsoluteTimestamp)
		}
		videoDataSize += int64(message.Buf.Len())
	case rtmp.AUDIO_TYPE:
		if flvFile != nil {
			flvFile.WriteAudioTag(message.Buf.Bytes(), message.AbsoluteTimestamp)
		}
		audioDataSize += int64(message.Buf.Len())
	}
}

func (handler *TestOutboundConnHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	fmt.Printf("ReceviedCommand: %+v\n", command)
}

func (handler *TestOutboundConnHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	fmt.Printf("Stream created: %d\n", stream.ID())
	createStreamChan <- stream
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "%s version[%s]\r\nUsage: %s [OPTIONS]\r\n", programName, version, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	fmt.Printf("rtmp:%s stream:%s flv:%s\r\n", *url, *streamName, *dumpFlv)
	l := log.NewLogger(".", "player", nil, 60, 3600*24, true)
	rtmp.InitLogger(l)
	defer l.Close()
	// Create flv file
	if len(*dumpFlv) > 0 {
		var err error
		flvFile, err = flv.CreateFile(*dumpFlv)
		if err != nil {
			fmt.Println("Create FLV dump file error:", err)
			return
		}
	}
	defer func() {
		if flvFile != nil {
			flvFile.Close()
		}
	}()

	createStreamChan = make(chan rtmp.OutboundStream)
	testHandler := &TestOutboundConnHandler{}
	fmt.Println("to dial")

	var err error

	obConn, err = rtmp.Dial(*url, testHandler, 100)
	/*
		conn := TryHandshakeByVLC()
		obConn, err = rtmp.NewOutbounConn(conn, *url, testHandler, 100)
	*/
	if err != nil {
		fmt.Println("Dial error", err)
		os.Exit(-1)
	}

	defer obConn.Close()
	fmt.Printf("obConn: %+v\n", obConn)
	fmt.Printf("obConn.URL(): %s\n", obConn.URL())
	fmt.Println("to connect")
	//	err = obConn.Connect("33abf6e996f80e888b33ef0ea3a32bfd", "131228035", "161114738", "play", "", "", "1368083579")
	err = obConn.Connect()
	if err != nil {
		fmt.Printf("Connect error: %s", err.Error())
		os.Exit(-1)
	}
	for {
		select {
		case stream := <-createStreamChan:
			// Play
			err = stream.Play(*streamName, nil, nil, nil)
			if err != nil {
				fmt.Printf("Play error: %s", err.Error())
				os.Exit(-1)
			}
			// Set Buffer Length

		case <-time.After(1 * time.Second):
			fmt.Printf("Audio size: %d bytes; Vedio size: %d bytes\n", audioDataSize, videoDataSize)
		}
	}
}

// //////////////////////////////////////////
func TryHandshakeByVLC() net.Conn {
	// listen
	listen, err := net.Listen("tcp", ":1935")
	if err != nil {
		fmt.Println("Listen error", err)
		os.Exit(-1)
	}
	defer listen.Close()

	iconn, err := listen.Accept()
	if err != nil {
		fmt.Println("Accept error", err)
		os.Exit(-1)
	}
	if iconn == nil {
		fmt.Println("iconn is nil")
		os.Exit(-1)
	}
	defer iconn.Close()
	// Handshake
	// C>>>P: C0+C1
	ibr := bufio.NewReader(iconn)
	ibw := bufio.NewWriter(iconn)
	c0, err := ibr.ReadByte()
	if c0 != 0x03 {
		fmt.Printf("C>>>P: C0(0x%2x) != 0x03\n", c0)
		os.Exit(-1)
	}
	c1 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(ibr, c1, rtmp.RTMP_SIG_SIZE)
	// Check C1
	var clientDigestOffset uint32
	if clientDigestOffset, err = CheckC1(c1, true); err != nil {
		fmt.Println("C>>>P: Test C1 err:", err)
		os.Exit(-1)
	}
	// P>>>S: Connect Server
	oconn, err := net.Dial("tcp", "192.168.20.111:1935")
	if err != nil {
		fmt.Println("P>>>S: Dial server err:", err)
		os.Exit(-1)
	}
	//	defer oconn.Close()
	obr := bufio.NewReader(oconn)
	obw := bufio.NewWriter(oconn)
	// P>>>S: C0+C1
	if err = obw.WriteByte(c0); err != nil {
		fmt.Println("P>>>S: Write C0 err:", err)
		os.Exit(-1)
	}
	if _, err = obw.Write(c1); err != nil {
		fmt.Println("P>>>S: Write C1 err:", err)
		os.Exit(-1)
	}
	if err = obw.Flush(); err != nil {
		fmt.Println("P>>>S: Flush err:", err)
		os.Exit(-1)
	}
	// P<<<S: Read S0+S1+S2
	s0, err := obr.ReadByte()
	if err != nil {
		fmt.Println("P<<<S: Read S0 err:", err)
		os.Exit(-1)
	}
	if c0 != 0x03 {
		fmt.Printf("P<<<S: S0(0x%2x) != 0x03\n", s0)
		os.Exit(-1)
	}
	s1 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(obr, s1, rtmp.RTMP_SIG_SIZE)
	if err != nil {
		fmt.Println("P<<<S: Read S1 err:", err)
		os.Exit(-1)
	}
	s2 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(obr, s2, rtmp.RTMP_SIG_SIZE)
	if err != nil {
		fmt.Println("P<<<S: Read S2 err:", err)
		os.Exit(-1)
	}

	// C<<<P: Send S0+S1+S2
	if err = ibw.WriteByte(s0); err != nil {
		fmt.Println("C<<<P: Write S0 err:", err)
		os.Exit(-1)
	}
	if _, err = ibw.Write(s1); err != nil {
		fmt.Println("C<<<P: Write S1 err:", err)
		os.Exit(-1)
	}
	if _, err = ibw.Write(s2); err != nil {
		fmt.Println("C<<<P: Write S2 err:", err)
		os.Exit(-1)
	}
	if err = ibw.Flush(); err != nil {
		fmt.Println("C<<<P: Flush err:", err)
		os.Exit(-1)
	}

	// C>>>P: Read C2
	c2 := make([]byte, rtmp.RTMP_SIG_SIZE)
	_, err = io.ReadAtLeast(ibr, c2, rtmp.RTMP_SIG_SIZE)

	// Check S2
	server_pos := rtmp.ValidateDigest(s1, 8, rtmp.GENUINE_FP_KEY[:30])
	if server_pos == 0 {
		server_pos = rtmp.ValidateDigest(s1, 772, rtmp.GENUINE_FP_KEY[:30])
		if server_pos == 0 {
			fmt.Println("P<<<S: S1 position check error")
			os.Exit(-1)
		}
	}

	digest, err := rtmp.HMACsha256(c1[clientDigestOffset:clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FMS_KEY)
	rtmp.CheckError(err, "Get digest from c1 error")

	signature, err := rtmp.HMACsha256(s2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digest)
	rtmp.CheckError(err, "Get signature from s2 error")

	if bytes.Compare(signature, s2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		fmt.Println("Server signature mismatch")
		os.Exit(-1)
	}

	digestResp, err := rtmp.HMACsha256(s1[server_pos:server_pos+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FP_KEY)
	rtmp.CheckError(err, "Generate C2 HMACsha256 digestResp")
	signatureResp, err := rtmp.HMACsha256(c2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digestResp)
	if bytes.Compare(signatureResp, c2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		fmt.Println("C2 mismatch")
		os.Exit(-1)
	}

	// P>>>S: Send C2
	if _, err = obw.Write(c2); err != nil {
		fmt.Println("P>>>S: Write C2 err:", err)
		os.Exit(-1)
	}
	if err = obw.Flush(); err != nil {
		fmt.Println("P>>>S: Flush err:", err)
		os.Exit(-1)
	}
	return oconn
}
func CheckC1(c1 []byte, offset1 bool) (uint32, error) {
	var clientDigestOffset uint32
	if offset1 {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 8, 728, 12)
	} else {
		clientDigestOffset = rtmp.CalcDigestPos(c1, 772, 728, 776)
	}
	// Create temp buffer
	tmpBuf := new(bytes.Buffer)
	tmpBuf.Write(c1[:clientDigestOffset])
	tmpBuf.Write(c1[clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH:])
	// Generate the hash
	tempHash, err := rtmp.HMACsha256(tmpBuf.Bytes(), rtmp.GENUINE_FP_KEY[:30])
	if err != nil {
		return 0, errors.New(fmt.Sprintf("HMACsha256 err: %s\n", err.Error()))
	}
	expect := c1[clientDigestOffset : clientDigestOffset+rtmp.SHA256_DIGEST_LENGTH]
	if bytes.Compare(expect, tempHash) != 0 {
		return 0, errors.New(fmt.Sprintf("C1\nExpect % 2x\nGot    % 2x\n",
			expect,
			tempHash))
	}
	return clientDigestOffset, nil
}

func CheckC2(s1, c2 []byte) (uint32, error) {
	server_pos := rtmp.ValidateDigest(s1, 8, rtmp.GENUINE_FMS_KEY[:36])
	if server_pos == 0 {
		server_pos = rtmp.ValidateDigest(s1, 772, rtmp.GENUINE_FMS_KEY[:36])
		if server_pos == 0 {
			return 0, errors.New("Server response validating failed")
		}
	}

	digest, err := rtmp.HMACsha256(s1[server_pos:server_pos+rtmp.SHA256_DIGEST_LENGTH], rtmp.GENUINE_FP_KEY)
	rtmp.CheckError(err, "Get digest from s1 error")

	signature, err := rtmp.HMACsha256(c2[:rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH], digest)
	rtmp.CheckError(err, "Get signature from c2 error")

	if bytes.Compare(signature, c2[rtmp.RTMP_SIG_SIZE-rtmp.SHA256_DIGEST_LENGTH:]) != 0 {
		return 0, errors.New("Server signature mismatch")
	}
	return server_pos, nil
}
