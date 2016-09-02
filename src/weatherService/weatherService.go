package main

import (
	"encoding/json"
	"errors"
	"github.com/go-kit/kit/endpoint"
	httptransport "github.com/go-kit/kit/transport/http"
	"golang.org/x/net/context"
	"gopkg.in/redis.v3"
	"log"
	"net/http"
	"os"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"time"
	"fmt"
	"strconv"
	"io"
)

var requestQueue  string
var responseQueue string
var waitTimeMultiplier int


type WeatherService interface {
	SubmitForecastRequest(*redis.Client, *sqs.SQS, forecastRequest) (forecastResponse, error)
	CheckForecastResponse(*redis.Client, forecastRequest) (forecastResponse)
}

type weatherService struct{}

func (weatherService) SubmitForecastRequest(redisClient *redis.Client, sqsClient *sqs.SQS, req forecastRequest) (forecastResponse, error) {

	var r forecastResponse

	b, err := json.Marshal(req)
	if err != nil {
		r.Err = err.Error()
		return r, err
	}

	// send response to message channel
	paramsSend := &sqs.SendMessageInput{
			MessageBody:  aws.String(string(b)),
			QueueUrl:     aws.String(requestQueue),
			DelaySeconds: aws.Int64(1),
	}
	rMess, err := sqsClient.SendMessage(paramsSend)
	if err != nil {
			r.Err = err.Error()
			return r, err
	}
	r.RequestID = *rMess.MessageId
	r.Status = 1

	// add hostname to every response so we can see load-balencing in play!
	r.Server, _ = os.Hostname()

	return r, nil
}

func (weatherService) CheckForecastResponse(redisClient *redis.Client, req forecastRequest) (forecastResponse) {

	var r forecastResponse
	var forecast string
	var err error
	if len(req.Lat) > 0 && len(req.Lon) > 0 {
		forecast, err = redisClient.Get("weatherService:Cache:" + req.Lat + ":" + req.Lon + ":" + req.ForecastType + ":response").Result()
	} else {
		forecast, err = redisClient.Get("weatherService:Cache:" + req.RequestID + ":response").Result()
	}
	
	if err != nil {
		r.Status = 1
		if len(req.RequestID) > 0 {
			r.RequestID = req.RequestID
		}
	} else {
		r.Summary = forecast
		r.Status = 0
	}

	// add hostname to every response so we can see load-balencing in play!
	r.Server, _ = os.Hostname()

	return r
}

// ErrEmpty is returned when input string is empty
var ErrEmpty = errors.New("Require GPS lat/long as input parameters")

type forecastRequest struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
	ForecastType string `json:"forecastType,omitempty"`
	RequestID string `json:"requestID"`
}

type forecastResponse struct {
	Summary string `json:"summary,omitempty"`
	Err     string `json:"err,omitempty"` // errors don't define JSON marshaling
	Status  int `json: status`
	RequestID string `json:"requestID,omitempty"`
	Server  string `json:"server,omitempty"`
}

type weatherResponse struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
  RequestID string `json:"requestID"`
  Forecast string `json:"forecast"`
	ForecastType string `json:"forecastType"`
}

func makeForecastEndpoint(svc WeatherService, redisClient *redis.Client, sqsClient *sqs.SQS, forecastType string) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(forecastRequest)
		var err error

		req.ForecastType = forecastType
		// always check for a cached forecast request
		v := svc.CheckForecastResponse(redisClient, req)
		if v.Status == 0 {
			return v, nil
		}

		// no cache so submit request onto queue
		v, err = svc.SubmitForecastRequest(redisClient, sqsClient, req)
		if err != nil {
			return v, err
		}

		// rather than just respond, wait then check if the request has already been processed
		// if it has we can return the actual forecast, otherwise we just return the requestID already obtained
		time.Sleep(time.Duration(waitTimeMultiplier) * time.Millisecond)
		v2 := svc.CheckForecastResponse(redisClient, req)
		if v2.Status == 0 {
			return v2, nil
		}
		return v, nil
	}
}

func processResponse (redisClient *redis.Client, sqsClient *sqs.SQS, message *sqs.Message) {

	var r weatherResponse
  if err := json.Unmarshal([]byte(*message.Body), &r); err != nil {
    return
  }

	err1 := redisClient.Set("weatherService:Cache:" + r.RequestID + ":response", r.Forecast, time.Duration(30)*time.Minute).Err()
	err2 := redisClient.Set("weatherService:Cache:" + r.Lat + ":" + r.Lon + ":" + r.ForecastType + ":response", r.Forecast, time.Duration(30)*time.Minute).Err()

	if err1 != nil || err2 != nil {
		return // give up something failed.
	}

	// signal back we have now dealt with the request to the request queue
	paramsDelete := &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(responseQueue),
			ReceiptHandle: aws.String(*message.ReceiptHandle),
	}
	_ , _ = sqsClient.DeleteMessage(paramsDelete)
}

func processQueueResponses(redisClient *redis.Client, sqsClient *sqs.SQS) {

	params := &sqs.ReceiveMessageInput{
      QueueUrl: aws.String(responseQueue),
      AttributeNames: []*string{
          aws.String(".*"),
      },
      MaxNumberOfMessages: aws.Int64(1),
      VisibilityTimeout: aws.Int64(20),
      WaitTimeSeconds:   aws.Int64(20),
  }

	for true {
    resp, err := sqsClient.ReceiveMessage(params)
    if err != nil {
      fmt.Println(err.Error())
    } else {
      for _, message := range resp.Messages {
          go processResponse(redisClient, sqsClient, message)
      }
    }
  }

}


func main() {

	requestQueue = os.Getenv("REQUEST_QUEUE")
	responseQueue = os.Getenv("RESPONSE_QUEUE")

	waitTime, err := strconv.ParseInt(os.Getenv("WAIT_TIME"), 10, 32)
	if err != nil {
		waitTimeMultiplier = 100
	} else {
		waitTimeMultiplier = int(waitTime)
	}

	ctx := context.Background()
	svc := weatherService{}

	// initiate Redis client pool and test connection
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_SERVER"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	sqsClient := sqs.New(session.New(), &aws.Config{Region: aws.String(os.Getenv("REGION"))})

	// make sure we can communicate to redis
	_, err = redisClient.Ping().Result()
	if err != nil {
		log.Fatal(err)
	}

	// setup mesage queue response handler
	go processQueueResponses(redisClient,sqsClient)

	forecastHandler := httptransport.NewServer(
		ctx,
		makeForecastEndpoint(svc, redisClient, sqsClient, os.Getenv("FORECAST_TYPE")),
		decodeForecastRequest,
		encodeResponse,
	)

	http.Handle("/forecast/" + os.Getenv("FORECAST_TYPE"), forecastHandler)
	http.HandleFunc("/", healthCheck)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func healthCheck(w http.ResponseWriter, req *http.Request) {
	io.WriteString(w, "healthy!\n")
}


func decodeForecastRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var request forecastRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, err
	}
	return request, nil
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}
