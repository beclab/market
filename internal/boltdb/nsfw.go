package boltdb

import (
	"fmt"
	"market/internal/conf"

	"github.com/boltdb/bolt"
	"github.com/golang/glog"
)

func SetNsfwState(nsfw bool) error {
	err := bdbClient.bdb.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(LocalDevNsfwBucketName))
		if err != nil {
			glog.Warningf("create bucket %s,err: %s", LocalDevNsfwBucketName, err.Error())
			return err
		}

		value := []byte{0}
		if nsfw {
			value[0] = 1
		}

		err = bucket.Put([]byte(NsfwKey), value)
		if err != nil {
			glog.Warningf("bucket.Put %s,err: %s", LocalDevNsfwBucketName, err.Error())
			return err
		}
		return nil
	})

	return err
}

func GetNsfwState() bool {
	if conf.GetIsPublic() {
		return true
	}
	isNsfw := true
	_ = bdbClient.bdb.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(LocalDevNsfwBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevNsfwBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevNsfwBucketName)
		}

		value := bucket.Get([]byte(NsfwKey))

		if len(value) <= 0 {
			//glog.Warningf("value %s not found", NsfwKey)
			return fmt.Errorf("value %s not found", NsfwKey)
		}

		isNsfw = value[0] == 1
		glog.Infof("value of 'nsfw': %v\n", isNsfw)
		return nil
	})

	return isNsfw
}
