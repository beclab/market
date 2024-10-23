package monitor

import (
	"encoding/json"
	"fmt"
	"market/internal/bfl"
	"market/internal/event"
	"time"

	"github.com/golang/glog"
	"go.etcd.io/bbolt"
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

var eventClient *event.Client = nil

func Start() {

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

	// open or create BoltDB
	db, err := bbolt.Open("./data/states.db", 0600, nil)
	if err != nil {
		glog.Fatalf("Error opening database: %v", err)
		return
	}
	defer db.Close()

	// save entrance state
	err = db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte("EntranceStates"))
		if err != nil {
			return err
		}

		for _, item := range response.Data.Data.Items {
			for _, entrance := range item.Entrances {
				// get state from db
				currentState := bucket.Get([]byte(entrance.ID))

				// if state not same，print item
				if currentState != nil && string(currentState) != entrance.State {
					itemJSON, _ := json.MarshalIndent(item, "", "  ")
					glog.Infof("Item with ID %s has inconsistent state:\n%s\n", entrance.ID, itemJSON)

					eventClient.CreateEvent("entrance-state-event",
						fmt.Sprintf("App %s entrance %s state is change to %s", item.Name, entrance.Name, entrance.State),
						item)
				}

				// save state to db
				err := bucket.Put([]byte(entrance.ID), []byte(entrance.State))
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		glog.Fatalf("Error updating database: %v", err)
	}

	glog.Info("States have been stored in BoltDB.")

}
