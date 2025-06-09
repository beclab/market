package redisdb

import (
	"encoding/json"
	"fmt"
	"log"
	"market/internal/models"
	"market/internal/conf"
)

func UpsertLocalAppInfo(info *models.ApplicationInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}

	err = redisClient.rdb.Set(ctx, info.Name, data, 0).Err() // 0 表示不设置过期时间
	if err != nil {
		log.Printf("redisClient.rdb.Set %s, err: %s", info.Name, err.Error())
		return err
	}
	return nil
}

func DelLocalAppInfo(key string) error {
	err := redisClient.rdb.Del(ctx, key).Err()
	if err != nil {
		log.Printf("redisClient.rdb.Del %s, err: %s", key, err.Error())
		return err
	}
	return nil
}

func ExistLocalAppInfo(key string) bool {
	val, err := redisClient.rdb.Exists(ctx, key).Result()
	if err != nil {
		log.Printf("redisClient.rdb.Exists %s, err: %s", key, err.Error())
		return false
	}
	return val > 0
}

func GetLocalAppInfo(key string) (*models.ApplicationInfo, error) {
	data, err := redisClient.rdb.Get(ctx, key).Result()
	if err != nil {
		log.Printf("redisClient.rdb.Get %s, err: %s", key, err.Error())
		return nil, fmt.Errorf("key '%s' not exist", key)
	}

	info := &models.ApplicationInfo{}
	err = json.Unmarshal([]byte(data), info)
	if err != nil {
		return nil, err
	}
	return info, nil
}

func GetLocalAppInfos(keys []string) []*models.ApplicationInfo {
	var infos []*models.ApplicationInfo

	data, err := redisClient.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("redisClient.rdb.MGet err: %v", err)
		return nil
	}

	for _, item := range data {
		if item == "" {
			continue
		}

		strItem, ok := item.(string)
		if !ok {
			log.Printf("item is not a string: %v", item)
			continue
		}

		info := &models.ApplicationInfo{}
		err = json.Unmarshal([]byte(strItem), info)
		if err != nil {
			log.Printf("err:%v", err)
			continue
		}
		infos = append(infos, info)
	}

	return infos
}

func GetLocalAppInfoMap() (map[string]*models.ApplicationInfo, error) {
	appMap := make(map[string]*models.ApplicationInfo)
	log.Println("Initializing GetLocalAppInfoMap")

	if conf.GetIsPublic() {
		return appMap, nil
	}

	keys, err := redisClient.rdb.Keys(ctx, "*").Result()
	if err != nil {
		log.Printf("Error getting keys: %v", err)
		return nil, err
	}

	data, err := redisClient.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		log.Printf("Error getting values: %v", err)
		return nil, err
	}

	for i, item := range data {
		if item == "" {
			continue
		}

		strItem, ok := item.(string)
		if !ok {
			log.Printf("item is not a string: %v", item)
			continue
		}

		info := &models.ApplicationInfo{}
		err = json.Unmarshal([]byte(strItem), info)
		if err != nil {
			log.Printf("Failed to unmarshal value for key %s: %v", keys[i], err)
			continue
		}
		appMap[info.Name] = info
		log.Printf("Added models.ApplicationInfo to map: %s", info.Name)
	}

	return appMap, nil
}
