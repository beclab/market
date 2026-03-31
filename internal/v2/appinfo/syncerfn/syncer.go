package syncerfn

import "context"

type Syncer struct{}

func (s *Syncer) Run(ctx context.Context) error {
	// todo 查询云端接口，同时对比数据库中的 app 数据，如果版本变化，更新数据库字段，需要标记为：未渲染，然后走 hydrator 流程
	return nil
}
