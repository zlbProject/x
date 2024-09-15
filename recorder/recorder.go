package recorder

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-gost/core/recorder"
)

const (
	RecorderServiceHandler       = "recorder.service.handler"
	RecorderServiceHandlerSerial = "recorder.service.handler.serial"
	RecorderServiceHandlerTunnel = "recorder.service.handler.tunnel"
)

type HTTPRequestRecorderObject struct {
	ContentLength int64       `json:"contentLength"`
	Header        http.Header `json:"header"`
}

type HTTPResponseRecorderObject struct {
	ContentLength int64       `json:"contentLength"`
	Header        http.Header `json:"header"`
}

type HTTPRecorderObject struct {
	Host       string                     `json:"host"`
	Method     string                     `json:"method"`
	Proto      string                     `json:"proto"`
	Scheme     string                     `json:"scheme"`
	URI        string                     `json:"uri"`
	StatusCode int                        `json:"statusCode"`
	Request    HTTPRequestRecorderObject  `json:"request"`
	Response   HTTPResponseRecorderObject `json:"response"`
}

type DNSRecorderObject struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Class    string `json:"class"`
	Type     string `json:"type"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Cached   bool   `json:"cached"`
}

type HandlerRecorderObject struct {
	Node       string              `json:"node,omitempty"`
	Service    string              `json:"service"`
	Network    string              `json:"network"`
	RemoteAddr string              `json:"remote"`
	LocalAddr  string              `json:"local"`
	Host       string              `json:"host"`
	ClientIP   string              `json:"clientIP"`
	ClientID   string              `json:"clientID,omitempty"`
	HTTP       *HTTPRecorderObject `json:"http,omitempty"`
	DNS        *DNSRecorderObject  `json:"dns,omitempty"`
	Err        string              `json:"err,omitempty"`
	Duration   time.Duration       `json:"duration"`
	Time       time.Time           `json:"time"`
	SID        string              `json:"sid"`
}

func (p *HandlerRecorderObject) Record(ctx context.Context, r recorder.Recorder) error {
	if p == nil || r == nil {
		return nil
	}

	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	return r.Record(ctx, data)
}
