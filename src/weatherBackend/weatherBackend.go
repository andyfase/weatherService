package main

import (
	"encoding/json"
  "os"
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/session"
  "github.com/aws/aws-sdk-go/service/sqs"
  "fmt"
  "net/http"
)


var inputQueue  string ="https://sqs.us-west-2.amazonaws.com/433468561249/weather-requests"
var outputQueue string ="https://sqs.us-west-2.amazonaws.com/433468561249/weather-responses"

type weatherRequest struct {
  Lat string `json:"lat"`
  Lon string `json:"lon"`
  ForecastType []string `json:"summaries"`
}

type weatherResponse struct {
  RequestID string `json:"requestID"`
  Forecasts map[string]string `json:"forecasts"`
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

func getWeather(lat string, lon string, summaries []string) (map[string]string, error) {
	var data map[string]interface{}
  response := make(map[string]string)
	if err := getJson(weatherURL + lat + "," + lon, &data); err != nil {
		return response, err
	}
	for key := range summaries {
		h, ok := data[summaries[key]].(map[string]interface{})
		if ok {
			forecast, ok := h["summary"].(string)
			if ok {
				response[summaries[key]] = forecast
			}
		}
	}
	return response, nil
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

  var response QueueResponse
  response.message.Forecasts = forecast
  response.message.RequestID = *message.MessageId
  response.ReceiptHandle = *message.ReceiptHandle

  responseChannel <- response
}

func processMessageResponses(queue chan QueueResponse) {

  // setup SQS object
  svc := sqs.New(session.New(), &aws.Config{Region: aws.String("us-west-2")})

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
  svc := sqs.New(session.New(), &aws.Config{Region: aws.String("us-west-2")})

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
