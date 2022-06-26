package replay

import (
	"bytes"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/up9inc/mizu/agent/pkg/app"
	"github.com/up9inc/mizu/shared"
	tapApi "github.com/up9inc/mizu/tap/api"
	mizuhttp "github.com/up9inc/mizu/tap/extensions/http"
)

var (
	inProcessRequestsLocker = sync.Mutex{}
	inProcessRequests       = 0
)

const (
	maxParallelAction      = 5
	timeoutForSingleAction = time.Second * 120
)

func canMakeRequest() bool {
	result := false
	inProcessRequestsLocker.Lock()
	if inProcessRequests < maxParallelAction {
		inProcessRequests++
		result = true
	}
	inProcessRequestsLocker.Unlock()
	return result
}

func ExecuteRequest(replayData *shared.ReplayDetails) (*tapApi.EntryWrapper, error) {
	if canMakeRequest() {
		defer decrementCounter()
		client := &http.Client{
			Timeout: timeoutForSingleAction,
		}

		request, err := http.NewRequest(replayData.Method, replayData.Url, bytes.NewBufferString(replayData.Body))
		if err != nil {
			return nil, err
		}

		for headerKey, headerValue := range replayData.Headers {
			request.Header.Add(headerKey, headerValue)
		}
		request.Header.Add("x-mizu-correlation-id", uuid.New().String())
		response, requestErr := client.Do(request)

		if requestErr != nil {
			return nil, requestErr
		}

		captureTime := time.Now()
		extension := app.ExtensionsMap["http"]

		item := tapApi.OutputChannelItem{
			Protocol: *extension.Protocol,
			ConnectionInfo: &tapApi.ConnectionInfo{
				ClientIP:   "",
				ClientPort: "1",
				ServerIP:   "",
				ServerPort: "1",
				IsOutgoing: false,
			},
			Capture:   "",
			Timestamp: time.Now().UnixMilli(),
			Pair: &tapApi.RequestResponsePair{
				Request: tapApi.GenericMessage{
					IsRequest:   true,
					CaptureTime: captureTime,
					CaptureSize: 0,
					Payload: mizuhttp.HTTPPayload{
						Type: mizuhttp.TypeHttpRequest,
						Data: request,
					},
				},
				Response: tapApi.GenericMessage{
					IsRequest:   false,
					CaptureTime: captureTime,
					CaptureSize: 0,
					Payload: mizuhttp.HTTPPayload{
						Type: mizuhttp.TypeHttpResponse,
						Data: response,
					},
				},
			},
		}

		entry := *extension.Dissector.Analyze(&item, "", "", "")
		base := extension.Dissector.Summarize(&entry)
		var representation []byte
		representation, err = extension.Dissector.Represent(entry.Request, entry.Response)
		if err != nil {
			return nil, err
		}

		return &tapApi.EntryWrapper{
			Protocol:       *extension.Protocol,
			Representation: string(representation),
			Data:           &entry,
			Base:           base,
			Rules:          nil,
			IsRulesEnabled: false,
		}, nil

	} else {
		return nil, errors.New("busy in too manu requests")
	}
}

func decrementCounter() {
	inProcessRequestsLocker.Lock()
	inProcessRequests--
	inProcessRequestsLocker.Unlock()
}
