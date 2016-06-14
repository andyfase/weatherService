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
	"time"
)

var weatherURL string = "https://api.forecast.io/forecast/" + os.Getenv("APIKEY")

type WeatherService interface {
	HourlyForecast(*redis.Client, string, string) (string, error)
	//	DailyForecast(string, string) (string, error)
}

func getJson(url string, target interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(target)
}

func getWeather(url string, forecasts map[string]string) error {
	var data map[string]interface{}
	if err := getJson(url, &data); err != nil {
		return err
	}
	for key, _ := range forecasts {
		h, ok := data[key].(map[string]interface{})
		if ok {
			forecast, ok := h["summary"].(string)
			if ok {
				forecasts[key] = forecast
			}
		}
	}
	return nil
}

type weatherService struct{}

func (weatherService) HourlyForecast(redisClient *redis.Client, lat string, lon string) (string, error) {

	forecasts := map[string]string{
		"hourly": "",
	}

	// check cache first
	forecast, redisErr := redisClient.Get("weatherService:Cache:" + lat + ":" + lon).Result()
	if redisErr == nil {
		return forecast, redisErr
	} else { // cache miss - fetch from backend
		err := getWeather(weatherURL+"/"+lat+","+lon, forecasts)
		if err == nil {
			// set cache with 5 minute validity
			redisErr := redisClient.Set("weatherService:Cache:"+lat+":"+lon, forecasts["hourly"], time.Duration(300)*time.Second).Err()
			if redisErr != nil {
				return " ", redisErr
			} else {
				return forecasts["hourly"], err
			}
		} else {
			return " ", err
		}
	}

}

// ErrEmpty is returned when input string is empty
var ErrEmpty = errors.New("Require GPS lat/long as input parameters")

type hourlyForecastRequest struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

type hourlyForecastResponse struct {
	Summary string `json:"summary"`
	Err     string `json:"err,omitempty"` // errors don't define JSON marshaling
}

func makeHourlyForecastEndpoint(svc WeatherService, redisClient *redis.Client) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(hourlyForecastRequest)
		v, err := svc.HourlyForecast(redisClient, req.Lat, req.Lon)
		if err != nil {
			return hourlyForecastResponse{v, err.Error()}, nil
		}
		return hourlyForecastResponse{v, ""}, nil
	}
}

func main() {
	ctx := context.Background()
	svc := weatherService{}

	// initiate Redis client pool and test connection
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_SERVER"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// make sure we can communicate to redis
	_, err := redisClient.Ping().Result()
	if err != nil {
		log.Fatal(err)
	}

	hourlyForecastHandler := httptransport.NewServer(
		ctx,
		makeHourlyForecastEndpoint(svc, redisClient),
		decodeHourlyForecastRequest,
		encodeResponse,
	)

	http.Handle("/forecast/hour", hourlyForecastHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func decodeHourlyForecastRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var request hourlyForecastRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, err
	}
	return request, nil
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}
