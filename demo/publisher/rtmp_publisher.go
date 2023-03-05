package main

import (
	"flag"
	"os"
	"time"

	"github.com/golang/glog"
	rtmp "github.com/wangyoucao577/gortmp"
	flv "github.com/zhangpeihao/goflv"
)

var (
	url         *string = flag.String("url", "rtmp://localhost:1935/live/", "The rtmp url to connect.")
	streamName  *string = flag.String("stream", "", "Stream name to play.")
	flvFileName *string = flag.String("flv", "1.flv", "FLV file to publish.")
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
	if obConn == nil {
		return
	}
	status, err = obConn.Status()
	glog.Infof("@@@@@@@@@@@@@status: %d, err: %v\n", status, err)
}

func (handler *TestOutboundConnHandler) OnClosed(conn rtmp.Conn) {
	glog.Infof("@@@@@@@@@@@@@Closed\n")
}

func (handler *TestOutboundConnHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
}

func (handler *TestOutboundConnHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	glog.Infof("ReceviedRtmpCommand: %+v\n", command)
}

func (handler *TestOutboundConnHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	glog.Infof("Stream created: %d\n", stream.ID())
	createStreamChan <- stream
}
func (handler *TestOutboundConnHandler) OnPlayStart(stream rtmp.OutboundStream) {

}
func (handler *TestOutboundConnHandler) OnPublishStart(stream rtmp.OutboundStream) {
	// Set chunk buffer size
	go publish(stream)
}

func publish(stream rtmp.OutboundStream) {
	glog.Infoln("1")
	var err error
	flvFile, err = flv.OpenFile(*flvFileName)
	if err != nil {
		glog.Infof("Open FLV dump file error:", err)
		return
	}
	glog.Infoln("2")
	defer flvFile.Close()
	startTs := uint32(0)
	startAt := time.Now().UnixNano()
	preTs := uint32(0)
	glog.Infoln("3")
	for status == rtmp.OUTBOUND_CONN_STATUS_CREATE_STREAM_OK {
		if flvFile.IsFinished() {
			glog.Info("@@@@@@@@@@@@@@File finished")
			flvFile.LoopBack()
			startAt = time.Now().UnixNano()
			startTs = uint32(0)
			preTs = uint32(0)
		}
		header, data, err := flvFile.ReadTag()
		if err != nil {
			glog.Errorf("flvFile.ReadTag() error:", err)
			break
		}
		switch header.TagType {
		case flv.VIDEO_TAG:
			videoDataSize += int64(len(data))
		case flv.AUDIO_TAG:
			audioDataSize += int64(len(data))
		}

		if startTs == uint32(0) {
			startTs = header.Timestamp
		}
		diff1 := uint32(0)
		//		deltaTs := uint32(0)
		if header.Timestamp > startTs {
			diff1 = header.Timestamp - startTs
		} else {
			glog.Warningf("@@@@@@@@@@@@@@diff1 header(%+v), startTs: %d\n", header, startTs)
		}
		if diff1 > preTs {
			//			deltaTs = diff1 - preTs
			preTs = diff1
		}
		glog.Warningf("@@@@@@@@@@@@@@diff1 header(%+v), startTs: %d\n", header, startTs)
		if err = stream.PublishData(header.TagType, data, diff1); err != nil {
			glog.Errorf("PublishData() error:", err)
			break
		}
		diff2 := uint32((time.Now().UnixNano() - startAt) / 1000000)
		//		fmt.Printf("diff1: %d, diff2: %d\n", diff1, diff2)
		if diff1 > diff2+100 {
			//			fmt.Printf("header.Timestamp: %d, now: %d\n", header.Timestamp, time.Now().UnixNano())
			time.Sleep(time.Millisecond * time.Duration(diff1-diff2))
		}
	}
}

func main() {
	flag.Parse()
	defer glog.Flush()

	createStreamChan = make(chan rtmp.OutboundStream)
	testHandler := &TestOutboundConnHandler{}
	glog.Info("to dial")
	glog.Info("a")
	var err error
	obConn, err = rtmp.Dial(*url, testHandler, 100)
	if err != nil {
		glog.Errorf("Dial error", err)
		os.Exit(-1)
	}
	glog.Info("b")
	defer obConn.Close()
	glog.Info("to connect")
	err = obConn.Connect()
	if err != nil {
		glog.Errorf("Connect error: %s", err.Error())
		os.Exit(-1)
	}
	glog.Info("c")
	for {
		select {
		case stream := <-createStreamChan:
			// Publish
			stream.Attach(testHandler)
			err = stream.Publish(*streamName, "live")
			if err != nil {
				glog.Errorf("Publish error: %s", err.Error())
				os.Exit(-1)
			}

		case <-time.After(1 * time.Second):
			glog.Infof("Audio size: %d bytes; Vedio size: %d bytes\n", audioDataSize, videoDataSize)
		}
	}
}
