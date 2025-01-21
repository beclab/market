package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"market/internal/bfl"
	"market/internal/event"
	"os"
	"strconv"
	"time"

	"market/internal/watchdog"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
)

type Entrance struct {
	AuthLevel  string `json:"authLevel"`
	Icon       string `json:"icon"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	OpenMethod string `json:"openMethod"`
	State      string `json:"state"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Invisible  bool   `json:"invisible"`
}

type Item struct {
	Deployment      string     `json:"deployment"`
	Entrances       []Entrance `json:"entrances"`
	Icon            string     `json:"icon"`
	ID              string     `json:"id"`
	IsClusterScoped bool       `json:"isClusterScoped"`
	IsSysApp        bool       `json:"isSysApp"`
	MobileSupported bool       `json:"mobileSupported"`
	Name            string     `json:"name"`
	Namespace       string     `json:"namespace"`
	Owner           string     `json:"owner"`
	State           string     `json:"state"`
	Target          string     `json:"target"`
	Title           string     `json:"title"`
	URL             string     `json:"url"`
}

type Response struct {
	Code int `json:"code"`
	Data struct {
		Code int `json:"code"`
		Data struct {
			Items []Item `json:"items"`
		} `json:"data"`
	} `json:"data"`
}

type EventForSocket struct {
	Code int    `json:"code"`
	Type string `json:"type"`
	App  Item   `json:"app"`
}

var eventClient *event.Client = nil
var redisClient *redis.Client

func Start() {
	// Initialize Redis client
	redisDbNumberStr := os.Getenv("REDIS_DB_NUMBER")

	redisDbNumber, errAtoi := strconv.Atoi(redisDbNumberStr)
	if errAtoi != nil {
		glog.Errorf("Error converting REDIS_DB_NUMBER to int:", errAtoi)
		return
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_ADDRESS"),  // Redis server address
		Password: os.Getenv("REDIS_PASSWORD"), // No password set
		DB:       redisDbNumber,               // Use default DB
	})

	interval := 5 * time.Second
	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for range ticker.C {
		glog.Info("monitor tick")

		if eventClient == nil {
			eventClient = event.NewClient()
		}

		jsonData, err := bfl.GetMyAppsInternal(eventClient)
		if err != nil {
			glog.Warningf(err.Error())
			return
		}

		checkState(jsonData)
	}
}

func checkState(jsonData string) {
	// parse JSON
	var response Response
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		glog.Fatalf("Error parsing JSON: %v", err)
		return
	}

	ctx := context.Background()

	// save entrance state
	for _, item := range response.Data.Data.Items {
		for _, entrance := range item.Entrances {
			// get state from Redis
			currentState, err := redisClient.Get(ctx, entrance.ID).Result()
			if err == redis.Nil {
				currentState = "" // Key does not exist
			} else if err != nil {
				glog.Fatalf("Error getting state from Redis: %v", err)
				return
			}

			// if state not same，print item
			if currentState != "" && currentState != entrance.State {
				// itemJSON, _ := json.MarshalIndent(item, "", "  ")
				// glog.Infof("Item with ID %s has inconsistent state:\n%s\n", entrance.ID, itemJSON)

				eventClient.CreateEvent("entrance-state-event",
					fmt.Sprintf("App %s entrance %s state is change to %s", item.Name, entrance.Name, entrance.State),
					item)

				watchdog.BroadcastMessage(EventForSocket{
					Code: 200,
					Type: "entrance-state-event",
					App:  item,
				})

				if item.State == "initializing" {
					resp := watchdog.InstallOrUpgradeResp{
						Code: 200,
						Data: watchdog.InstallOrUpgradeStatus{
							Uid:      item.Name,
							Type:     "install",
							Status:   "initializing",
							Progress: "100",
							Msg:      "",
							From:     "",
						},
					}

					watchdog.BroadcastMessage(resp)
				}
			}

			// save state to Redis
			err = redisClient.Set(ctx, entrance.ID, entrance.State, 0).Err()
			if err != nil {
				glog.Fatalf("Error saving state to Redis: %v", err)
			}
		}
	}

	// glog.Info("States have been stored in Redis.")
}
