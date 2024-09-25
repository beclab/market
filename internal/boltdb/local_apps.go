package boltdb

import (
	"encoding/json"
	"fmt"
	"market/internal/conf"
	"market/internal/models"

	"github.com/boltdb/bolt"
	"github.com/golang/glog"
)

func UpsertLocalAppInfo(info *models.ApplicationInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	err = bdbClient.bdb.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(LocalDevAppsBucketName))
		if err != nil {
			glog.Warningf("create bucket %s,err: %s", LocalDevAppsBucketName, err.Error())
			return err
		}

		err = bucket.Put([]byte(info.Name), data)
		if err != nil {
			glog.Warningf("bucket.Put %s,err: %s", LocalDevAppsBucketName, err.Error())
			return err
		}
		return nil
	})

	return err
}

func DelLocalAppInfo(key string) (err error) {
	err = bdbClient.bdb.Update(func(tx *bolt.Tx) error {
		//if not exist, Delete do not return error
		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
		}

		err = bucket.Delete([]byte(key))
		if err != nil {
			return err
		}
		return nil
	})

	return err
}

func ExistLocalAppInfo(key string) bool {
	err := bdbClient.bdb.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
		}

		if bucket.Get([]byte(key)) == nil {
			glog.Warningf("bucket %s Get %s nil", LocalDevAppsBucketName, key)
			return fmt.Errorf("bucket %s key:%s not found", LocalDevAppsBucketName, key)
		}
		return nil
	})

	return err == nil
}

func GetLocalAppInfo(key string) (*models.ApplicationInfo, error) {
	if conf.GetIsPublic() {
		return nil, nil
	}
	info := &models.ApplicationInfo{}
	var err error
	err = bdbClient.bdb.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
		}

		data := bucket.Get([]byte(key))
		if data == nil {
			glog.Warningf("key '%s' not exist", key)
			return fmt.Errorf("key '%s' not exist", key)
		}

		err = json.Unmarshal(data, info)
		if err != nil {
			return err
		}
		return nil
	})

	if err == nil {
		return info, nil
	}

	return nil, err
}

func GetLocalAppInfos(keys []string) []*models.ApplicationInfo {
	var infos []*models.ApplicationInfo
	var err error
	err = bdbClient.bdb.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
		}

		for _, key := range keys {
			data := bucket.Get([]byte(key))
			if data == nil {
				//glog.Warningf("key '%s' not exist", key)
				continue
			}
			info := &models.ApplicationInfo{}
			err = json.Unmarshal(data, info)
			if err != nil {
				glog.Warningf("err:%v", err)
				continue
			}
			infos = append(infos, info)
		}

		return nil
	})

	return infos
}

//func GetDevAppInfoList(page, size int) (list []*models.ApplicationInfo, count int, err error) {
//	return GetAppInfoList(page, size, constants.AppFromDev)
//}

//func GetAppInfoList(page, size int, from string) (list []*models.ApplicationInfo, count int, err error) {
//	err = bdbClient.bdb.View(func(tx *bolt.Tx) error {
//		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
//		if bucket == nil {
//			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
//			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
//		}
//
//		cursor := bucket.Cursor()
//		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
//			if count < (page-1)*size {
//				glog.Infof("Key: %s, Value: %s skip\n", k, v)
//				count++
//				continue
//			}
//
//			glog.Infof("Key: %s, Value: %s\n", k, v)
//
//			if count <= page*size {
//				info := &models.ApplicationInfo{}
//				err = json.Unmarshal(v, info)
//				if err != nil {
//					return err
//				}
//				if from != "" && !strings.EqualFold(info.Source, from) {
//					continue
//				}
//				list = append(list, info)
//			}
//			count++
//		}
//
//		return nil
//	})
//
//	return
//}

func GetLocalAppInfoMap() (appMap map[string]*models.ApplicationInfo, err error) {
	appMap = make(map[string]*models.ApplicationInfo)
	glog.Infof("Initializing GetLocalAppInfoMap")

	err = bdbClient.bdb.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(LocalDevAppsBucketName))
		if bucket == nil {
			glog.Warningf("bucket %s not found", LocalDevAppsBucketName)
			return fmt.Errorf("bucket %s not found", LocalDevAppsBucketName)
		}

		glog.Infof("Successfully retrieved bucket: %s", LocalDevAppsBucketName)

		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			glog.Infof("Key: %s, Value: %s\n", k, v)

			info := &models.ApplicationInfo{}
			err = json.Unmarshal(v, info)
			if err != nil {
				glog.Errorf("Failed to unmarshal value for key %s: %v", k, err)
				continue
			}

			if info.Name == "" {
				glog.Warningf("ApplicationInfo for key %s has empty Name field", k)
				continue
			}

			appMap[info.Name] = info
			glog.Infof("Added ApplicationInfo to map: %s", info.Name)
		}

		glog.Infof("Finished processing bucket: %s", LocalDevAppsBucketName)
		return nil
	})

	if err != nil {
		glog.Errorf("Error during GetLocalAppInfoMap: %v", err)
	} else {
		glog.Infof("Successfully retrieved application info map with %d entries", len(appMap))
	}

	return
}
