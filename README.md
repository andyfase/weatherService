# weatherService
An example micro-service to fetch a summary of the weather given a lat/long input. Written in Go using go-kit, and written to test AWS ECS.


# Install 

1. [Install GO](https://golang.org/doc/install)
1. [Install Docker](https://docs.docker.com/engine/installation/)
1. (Needed for OSX and Windows) [install VirtualBox](https://www.virtualbox.org/wiki/Downloads) 
1. Install [aws-shell](https://github.com/awslabs/aws-shell) 

# Setup 

1. Clone repo `git clone https://github.com/andyfase/weatherService.git`
1. Run `export GOPATH=/whereever/you/put/it
1. Install Dependencies `go get ....` listed below.
1. Run `GOOS=linux GOARCH=amd64 go build -o ./bin/linux/weatherService weatherService` to build a linux binary
1. Setup your [docker-machine (needed for Windows and OSX)](https://docs.docker.com/machine/get-started/)
1. Run `docker build -t weather-service --file ./weatherService.scratch  .`

# Go Libaries

Run 

```
go get github.com/go-kit/kit/endpoint
go get github.com/go-kit/kit/transport/http
go get gopkg.in/redis.v3
```

# Setup ECS Private Docker registry

[Follow guides on AWS](http://docs.aws.amazon.com/AmazonECR/latest/userguide/ECR_GetStarted.html)

Once you have your own private repo you can the docker command line tool to add the remote registry, tag your local container and push it

```
aws-shell
ecr get-login
[QUIT aws-shell]
[run output of command]
docker tag weather-service:1.0 <your_ecr_repo>.amazonaws.com/<your_ecr_repo_name>/weather-service:1.0
docker push <your_ecr_repo>.amazonaws.com/<your_ecr_repo_name>:weather-service-1.0
```
# Runing Service
The service can be run locally through `docker run` but will exit out becuase it expects to connect to a Redis server (used for Caching)

Two environment variables are needed

- **REDIS_SERVER** The redis service to connect to in the format "host:port" (this is expected to be used with a [ElasticCache Redis server](http://docs.aws.amazon.com/AmazonElastiCache/latest/UserGuide/Clusters.Create.Redis.CON.html))
- **APIKEY** The weather service uses [Dark Skies](https://developer.forecast.io/) API to retrieve weather data. Signup (its free) and get your API ID to use.

# Go Build your ECS Cluster

[ECS Guide from AWS](http://docs.aws.amazon.com/AmazonECS/latest/developerguide/ECS_GetStarted.html)

Some notes on this:

- Make sure your security group allows connectivity between the servers for the ECS deamons to communicate
- Setup a ELB for the ECS service to load balance requests. The weatherService exposes port 8080 on the ECS cluster node. Hence the ELB needs to connect to port 8080 on the backend
- Make sure your security group allows the ELB to talk to the ECS cluster on port 8080 :-)
- Make sure you pass in those environment variables!!

# Usage
Calling the weatherService. Use curl or your tool of choice:

```
curl -d'{"lat":"49.2561055", "lon":"-123.1939531"}' http://weather.aws.andyfase.com/forecast/hour
```

Change the domain to your domain of choice, the one above does exist so you can use it as a reference too!

