package boltdb

import (
	"market/internal/constants"
	"market/pkg/utils"
	"path"

	"github.com/boltdb/bolt"
	"github.com/golang/glog"
)

type Client struct {
	bdb *bolt.DB
}

const (
	LocalDevAppsDbName     = "localDevApps.db"
	LocalDevAppsBucketName = "LocalDevAppsBucket"
	LocalDevNsfwBucketName = "LocalDevNsfwBucket"

	NsfwKey = "nsfw"
)

var (
	bdbClient *Client
)

func Init() error {
	//open or create db file
	LocalDevAppsDbPath := path.Join(constants.DataPath, LocalDevAppsDbName)
	err := utils.CheckDir(constants.DataPath)
	if err != nil {
		glog.Errorf("utils.CheckDir %s, err:%s", constants.DataPath, err.Error())
		return err
	}

	db, err := bolt.Open(LocalDevAppsDbPath, 0600, nil)
	if err != nil {
		glog.Errorf("bolt.Open %s, err:%s", LocalDevAppsDbPath, err.Error())
		return err
	}
	bdbClient = &Client{bdb: db}

	_, err = bdbClient.createBucket(LocalDevAppsBucketName)
	if err != nil {
		glog.Errorf("GetBucket %s, err:%s", LocalDevAppsBucketName, err.Error())
		return err
	}

	_, err = bdbClient.createBucket(LocalDevNsfwBucketName)
	if err != nil {
		glog.Errorf("GetBucket %s, err:%s", LocalDevNsfwBucketName, err.Error())
		return err
	}

	return nil
}

func (c *Client) createBucket(bucketName string) (bucket *bolt.Bucket, err error) {
	err = c.bdb.Update(func(tx *bolt.Tx) error {
		bucket, err = tx.CreateBucketIfNotExists([]byte(bucketName))
		if err != nil {
			glog.Warningf("create bucket %s,err: %s", bucketName, err.Error())
			return err
		}

		return nil
	})

	return bucket, err
}
