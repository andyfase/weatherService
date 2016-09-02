package main

import (
	"encoding/json"
  "os"
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/sqs"
  "fmt"
  "net/http"
  "time"
  "strconv"
	"errors"
)


var inputQueue  string
var outputQueue string
var processWait int

type weatherRequest struct {
  Lat string `json:"lat"`
  Lon string `json:"lon"`
  ForecastType string `json:"forecastType"`
}

type weatherResponse struct {
  Lat string `json:"lat"`
  Lon string `json:"lon"`
  RequestID string `json:"requestID"`
  Forecast string `json:"forecast"`
	ForecastType string `json:"forecastType"`
}

type QueueResponse struct {
  message weatherResponse
  ReceiptHandle string
}

var weatherURL string = "https://api.forecast.io/forecast/" + os.Getenv("APIKEY") + "/"

func getJson(url string, target interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func getWeather(lat string, lon string, forecastType string) (string, error) {
	var data map[string]interface{}
	if err := getJson(weatherURL + lat + "," + lon, &data); err != nil {
		return "", err
	}

	h, ok := data[forecastType].(map[string]interface{})
	if ok {
		forecast, ok := h["summary"].(string)
		if ok {
			return forecast, nil
		}
	}
	return "", errors.New("Could not extract forecast for: " + forecastType)
}

func processMessage (message *sqs.Message, responseChannel chan QueueResponse) {

  var r weatherRequest
  if err := json.Unmarshal([]byte(*message.Body), &r); err != nil {
    fmt.Println(err)
    return
  }

  forecast, err := getWeather(r.Lat, r.Lon, r.ForecastType)
  if err != nil {
    fmt.Println(err)
    return
  }

  // sleep configurable time
  time.Sleep(time.Duration(processWait) * time.Millisecond)

  var response QueueResponse
  response.message.Forecast = forecast
  response.message.RequestID = *message.MessageId
  response.ReceiptHandle = *message.ReceiptHandle
  response.message.Lat = r.Lat
  response.message.Lon = r.Lon
	response.message.ForecastType = r.ForecastType

  responseChannel <- response
}

func processMessageResponses(queue chan QueueResponse) {

  // setup SQS object
  svc := sqs.New(session.New(), &aws.Config{Region: aws.String(os.Getenv("REGION"))})

  // loop over channel waiting to deal with weather Responses
  for elem := range queue {

    // JSON encode
    b, err := json.Marshal(elem.message)
    if err != nil {
      fmt.Println(err)
      continue
    }

    // send response to message channel
    paramsSend := &sqs.SendMessageInput{
        MessageBody:  aws.String(string(b)),
        QueueUrl:     aws.String(outputQueue),
        DelaySeconds: aws.Int64(1),
    }
    _, err = svc.SendMessage(paramsSend)
    if err != nil {
        fmt.Println(err)
        continue
    }
    // signal back we have now dealt with the request to the request queue
    paramsDelete := &sqs.DeleteMessageInput{
        QueueUrl:      aws.String(inputQueue),
        ReceiptHandle: aws.String(elem.ReceiptHandle),
    }
    _, err = svc.DeleteMessage(paramsDelete)

    if err != nil {
        fmt.Println(err)
    }
  }
}

func main() {

  inputQueue  = os.Getenv("REQUEST_QUEUE")
  outputQueue = os.Getenv("RESPONSE_QUEUE")

  waitTime, err := strconv.ParseInt(os.Getenv("WAIT_TIME"), 10, 32)
  if err != nil {
    processWait = 0
  } else {
    processWait = int(waitTime)
  }

  svc := sqs.New(session.New(), &aws.Config{Region: aws.String(os.Getenv("REGION"))})

  params := &sqs.ReceiveMessageInput{
      QueueUrl: aws.String(inputQueue),
      AttributeNames: []*string{
          aws.String(".*"),
      },
      MaxNumberOfMessages: aws.Int64(1),
      VisibilityTimeout: aws.Int64(20),
      WaitTimeSeconds:   aws.Int64(20),
  }

  // create channel for dealing with responses
  responseChannel := make(chan QueueResponse, 100)
  go processMessageResponses(responseChannel)

  for true {
    resp, err := svc.ReceiveMessage(params)
    if err != nil {
      fmt.Println(err.Error())
    } else {
      for _, message := range resp.Messages {
          go processMessage(message, responseChannel)
      }
    }
  }

}
