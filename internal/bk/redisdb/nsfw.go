package redisdb

import (
	"market/internal/conf"

	"github.com/go-redis/redis/v8"
	"github.com/golang/glog"
)

func SetNsfwState(nsfw bool) error {
	value := 0
	if nsfw {
		value = 1
	}

	err := redisClient.rdb.Set(ctx, NsfwKey, value, 0).Err()
	if err != nil {
		glog.Warningf("redisClient.Set %s, err: %s", NsfwKey, err.Error())
		return err
	}
	return nil
}

func GetNsfwState() bool {
	if conf.GetIsPublic() {
		return true
	}
	isNsfw := true

	value, err := redisClient.rdb.Get(ctx, NsfwKey).Result()
	if err != nil {
		if err == redis.Nil {
			glog.Warningf("value %s not found", NsfwKey)
			return false
		}
		glog.Warningf("redisClient.Get %s, err: %s", NsfwKey, err.Error())
		return false
	}

	isNsfw = value == "1"
	glog.Infof("value of 'nsfw': %v\n", isNsfw)
	return isNsfw
}
