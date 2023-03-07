package main

import (
	"flag"
	"os"
	"time"

	"github.com/golang/glog"
	rtmp "github.com/wangyoucao577/gortmp"
	flv "github.com/zhangpeihao/goflv"
)

var flags struct {
	url           string
	streamName    string
	inputFilePath string
}

func init() {
	flag.StringVar(&flags.inputFilePath, "i", "", "FLV file path to publish, e.g., '1.flv'")
	flag.StringVar(&flags.streamName, "stream", "", "Stream name")
	flag.StringVar(&flags.url, "url", "rtmp://localhost:1935/live/", "The rtmp url to connect, e.g., 'rtmp://host[:port]/[appname[/instanceName]]'")
}

type TestOutboundConnHandler struct {
}

var obConn rtmp.OutboundConn
var createStreamChan chan rtmp.OutboundStream
var videoDataSize int64
var audioDataSize int64
var startPublishTime time.Time

var status uint

func (handler *TestOutboundConnHandler) OnStatus(conn rtmp.OutboundConn) {
	var err error
	if obConn == nil {
		return
	}
	status, err = obConn.Status()
	glog.Infof("OnStatus: %s(%d), err: %v\n", rtmp.OutboundConnStatusDescription(status), status, err)
}

func (handler *TestOutboundConnHandler) OnClosed(conn rtmp.Conn) {
	glog.Infof("OnClosed\n")
}

func (handler *TestOutboundConnHandler) OnReceived(conn rtmp.Conn, message *rtmp.Message) {
	glog.Infof("OnReceived: %+v\n", message)
}

func (handler *TestOutboundConnHandler) OnReceivedRtmpCommand(conn rtmp.Conn, command *rtmp.Command) {
	glog.Infof("OnReceivedRtmpCommand: %+v\n", command)
}

func (handler *TestOutboundConnHandler) OnStreamCreated(conn rtmp.OutboundConn, stream rtmp.OutboundStream) {
	glog.Infof("OnStreamCreated, stream: %d\n", stream.ID())
	createStreamChan <- stream
}
func (handler *TestOutboundConnHandler) OnPlayStart(stream rtmp.OutboundStream) {
	glog.Infof("OnPlayStart, stream: %d\n", stream.ID())
}
func (handler *TestOutboundConnHandler) OnPublishStart(stream rtmp.OutboundStream) {
	glog.Infof("OnPublishStart, stream: %d\n", stream.ID())

	// Set chunk buffer size
	go publish(stream)
}

func publish(stream rtmp.OutboundStream) {
	glog.Infof("publish, stream: %d\n", stream.ID())

	flvFile, err := flv.OpenFile(flags.inputFilePath)
	if err != nil {
		glog.Errorf("Open FLV dump file error: %v", err)
		return
	}
	defer flvFile.Close()

	startTs := uint32(0)
	startAt := time.Now().UnixNano()
	preTs := uint32(0)
	startPublishTime = time.Now()

	for status == rtmp.OUTBOUND_CONN_STATUS_CREATE_STREAM_OK {
		if flvFile.IsFinished() {
			glog.Info("flv file is finished, loopback.")
			flvFile.LoopBack()
			startAt = time.Now().UnixNano()
			startTs = uint32(0)
			preTs = uint32(0)
		}
		header, data, err := flvFile.ReadTag()
		if err != nil {
			glog.Errorf("flvFile.ReadTag() error: %v", err)
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
		if header.Timestamp >= startTs {
			diff1 = header.Timestamp - startTs
		} else {
			glog.Warningf("@@@@@@@@@@@@@@diff1 header(%+v), startTs: %d\n", header, startTs)
		}
		if diff1 > preTs {
			//			deltaTs = diff1 - preTs
			preTs = diff1
		}
		if glog.V(3) {
			glog.Infof("@@@@@@@@@@@@@@diff1 header(%+v), startTs: %d\n", header, startTs)
		}

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

	var err error
	createStreamChan = make(chan rtmp.OutboundStream)
	testHandler := &TestOutboundConnHandler{}

	glog.Info("to dial")
	obConn, err = rtmp.Dial(flags.url, testHandler, 100)
	if err != nil {
		glog.Errorf("Dial failed, err: %v", err)
		os.Exit(-1)
	}
	defer obConn.Close()

	glog.Info("to connect")
	err = obConn.Connect()
	if err != nil {
		glog.Errorf("Connect failed, err: %v", err)
		os.Exit(-1)
	}
	glog.Info("after connect")

	var lastAudioDataSize, lastVideoDataSize int64
	var lastTimeInSeconds float64
	for {
		select {
		case stream := <-createStreamChan:
			// Publish
			stream.Attach(testHandler)
			err = stream.Publish(flags.streamName, "live")
			if err != nil {
				glog.Errorf("Publish error: %s", err.Error())
				os.Exit(-1)
			}

		case <-time.After(1 * time.Second):
			calcKbps := func(bytes int64, seconds float64) int64 {
				return int64(float64(bytes) * 8 / 1000 / seconds)
			}
			aSize, vSize := audioDataSize, videoDataSize
			timeInSeconds := time.Since(startPublishTime).Seconds()
			aKbps, vKbps := calcKbps(aSize-lastAudioDataSize, timeInSeconds-lastTimeInSeconds), calcKbps(vSize-lastVideoDataSize, timeInSeconds-lastTimeInSeconds)
			glog.Infof("publish bitrate %d kbps(audio %d, video %d), totally sent %d bytes(audio %d, video %d) in %f seconds\n",
				aKbps+vKbps, aKbps, vKbps, aSize+vSize, aSize, vSize, timeInSeconds)

			lastAudioDataSize, lastVideoDataSize = aSize, vSize
			lastTimeInSeconds = timeInSeconds
		}
	}
}
